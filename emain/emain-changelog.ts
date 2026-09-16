// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import fs from "fs/promises";
import path from "path";
import {
    collectBareUrls,
    embedBareMedia,
    isAttachmentUrl,
    mediaKindFromContentType,
    mediaKindFromUrl,
} from "./changelog-media";
import { getWaveDataDir } from "./emain-platform";
import { getWaveVersion } from "./emain-wavesrv";

const ChangelogRepo = "petronijus/waveterm";
const ChangelogReleaseCount = 10;
const ChangelogCacheFileName = "changelog-cache.json";
const ChangelogFreshMs = 30 * 60 * 1000;
const ChangelogFetchTimeoutMs = 10000;

type ChangelogCache = {
    etag: string;
    fetchedat: number;
    entries: ChangelogEntry[];
};

let cache: ChangelogCache = null;
let cacheLoaded = false;
let inflight: Promise<ChangelogResult> = null;

function cacheFilePath(): string {
    return path.join(getWaveDataDir(), ChangelogCacheFileName);
}

async function loadCache(): Promise<ChangelogCache> {
    if (cacheLoaded) {
        return cache;
    }
    cacheLoaded = true;
    try {
        const raw = await fs.readFile(cacheFilePath(), "utf8");
        const parsed = JSON.parse(raw) as ChangelogCache;
        if (Array.isArray(parsed?.entries)) {
            cache = parsed;
        }
    } catch (e) {
        // no usable cache on disk -- the first fetch writes one
    }
    return cache;
}

async function saveCache(newCache: ChangelogCache): Promise<void> {
    cache = newCache;
    const filePath = cacheFilePath();
    const tmpPath = `${filePath}.tmp`;
    try {
        await fs.writeFile(tmpPath, JSON.stringify(newCache), "utf8");
        await fs.rename(tmpPath, filePath);
    } catch (e) {
        console.log("changelog: could not persist cache:", e.message ?? e);
    }
}

function toEntry(release: any): ChangelogEntry {
    return {
        tag: release.tag_name ?? "",
        name: release.name || release.tag_name || "",
        publishedat: release.published_at ?? release.created_at ?? "",
        body: (release.body ?? "").trim(),
        url: release.html_url ?? "",
        prerelease: !!release.prerelease,
    };
}

async function probeMediaKind(url: string): Promise<string> {
    const byExt = mediaKindFromUrl(url);
    if (byExt) {
        return byExt;
    }
    // only GitHub's own attachments are probed -- HEADing every link in a release body would
    // announce the app to every host somebody happened to link
    if (!isAttachmentUrl(url)) {
        return "";
    }
    try {
        const resp = await fetch(url, {
            method: "HEAD",
            redirect: "follow",
            signal: AbortSignal.timeout(ChangelogFetchTimeoutMs),
        });
        if (!resp.ok) {
            return "";
        }
        return mediaKindFromContentType(resp.headers.get("content-type") ?? "");
    } catch (e) {
        console.log("changelog: could not probe attachment:", e.message ?? e);
        return "";
    }
}

async function resolveMedia(entries: ChangelogEntry[]): Promise<void> {
    const urls = new Set<string>();
    for (const entry of entries) {
        for (const url of collectBareUrls(entry.body)) {
            urls.add(url);
        }
    }
    if (urls.size == 0) {
        return;
    }
    const kinds = new Map<string, string>();
    await Promise.all(
        [...urls].map(async (url) => {
            kinds.set(url, await probeMediaKind(url));
        })
    );
    for (const entry of entries) {
        entry.body = embedBareMedia(entry.body, kinds);
    }
}

type FetchResult = { entries: ChangelogEntry[]; etag: string; notmodified: boolean };

async function fetchReleases(etag: string): Promise<FetchResult> {
    const { version } = getWaveVersion();
    const headers: Record<string, string> = {
        accept: "application/vnd.github+json",
        "x-github-api-version": "2022-11-28",
        "user-agent": `WaveTerm/${version}`,
    };
    // a conditional request costs no rate limit when nothing changed, and the ETag is
    // what keeps a client that opens the changelog repeatedly off GitHub's 60/hour budget
    if (etag) {
        headers["if-none-match"] = etag;
    }
    const url = `https://api.github.com/repos/${ChangelogRepo}/releases?per_page=${ChangelogReleaseCount}`;
    const resp = await fetch(url, { headers, signal: AbortSignal.timeout(ChangelogFetchTimeoutMs) });
    if (resp.status === 304) {
        return { entries: null, etag, notmodified: true };
    }
    if (!resp.ok) {
        throw new Error(`GitHub responded ${resp.status} ${resp.statusText}`);
    }
    const releases = (await resp.json()) as any[];
    if (!Array.isArray(releases)) {
        throw new Error("unexpected response from GitHub");
    }
    const entries = releases
        .filter((release) => !release.draft)
        .slice(0, ChangelogReleaseCount)
        .map(toEntry);
    await resolveMedia(entries);
    return { entries, etag: resp.headers.get("etag") ?? "", notmodified: false };
}

async function loadChangelog(force: boolean): Promise<ChangelogResult> {
    const cached = await loadCache();
    if (cached && !force && Date.now() - cached.fetchedat < ChangelogFreshMs) {
        return { entries: cached.entries, fetchedat: cached.fetchedat, stale: false };
    }
    try {
        const resp = await fetchReleases(cached?.etag);
        const fetchedat = Date.now();
        const entries = resp.notmodified ? cached.entries : resp.entries;
        await saveCache({ etag: resp.etag, fetchedat, entries });
        return { entries, fetchedat, stale: false };
    } catch (e) {
        const errMsg = e.message ?? String(e);
        console.log("changelog: fetch failed:", errMsg);
        if (cached) {
            return { entries: cached.entries, fetchedat: cached.fetchedat, stale: true, error: errMsg };
        }
        return { entries: [], fetchedat: 0, stale: true, error: errMsg };
    }
}

export function getChangelog(force?: boolean): Promise<ChangelogResult> {
    if (inflight && !force) {
        return inflight;
    }
    // a forced refresh must not ride on a non-forced load, which may answer straight from
    // a still-fresh cache -- it queues behind it instead of joining it
    const run = inflight ? inflight.then(() => loadChangelog(!!force)) : loadChangelog(!!force);
    inflight = run;
    run.finally(() => {
        if (inflight === run) {
            inflight = null;
        }
    });
    return run;
}
