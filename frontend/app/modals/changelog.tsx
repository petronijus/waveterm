// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { Markdown } from "@/app/element/markdown";
import { atoms, getApi, openLink } from "@/app/store/global";
import { modalsModel } from "@/app/store/modalmodel";
import { isDev } from "@/util/isdev";
import { cn, fireAndForget } from "@/util/util";
import { useAtomValue } from "jotai";
import { OverlayScrollbarsComponent } from "overlayscrollbars-react";
import { useCallback, useEffect, useState } from "react";
import { Modal } from "./modal";

const ReleasesUrl = "https://github.com/petronijus/waveterm/releases";

function tagVersion(tag: string): string {
    return tag.replace(/^v/, "");
}

function entryLabel(entry: ChangelogEntry): string {
    const pjMatch = entry.tag.match(/-pj\.(\d+)$/);
    if (pjMatch) {
        return `pj.${pjMatch[1]}`;
    }
    return tagVersion(entry.tag) || entry.name;
}

// release names repeat the version they already carry ("v0.14.5-pj.25 — undo close tab"),
// so only the part that says something new survives into the header
function entryHeadline(entry: ChangelogEntry): string {
    return entry.name
        .replace(/^wave\s*\((?:pj|petronijus fork)\)\s*/i, "")
        .replace(/^v?\d+\.\d+\.\d+(?:-[0-9a-z.]+)?/i, "")
        .replace(/^[\s—–-]+/, "")
        .trim();
}

function formatDate(iso: string): string {
    if (!iso) {
        return "";
    }
    const date = new Date(iso);
    if (isNaN(date.getTime())) {
        return "";
    }
    return date.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
}

interface ChangelogEntryRowProps {
    entry: ChangelogEntry;
    isCurrent: boolean;
    isExpanded: boolean;
    onToggle: () => void;
}

const ChangelogEntryRow = ({ entry, isCurrent, isExpanded, onToggle }: ChangelogEntryRowProps) => {
    const headline = entryHeadline(entry);
    return (
        <div className="border-b border-border last:border-b-0">
            <button
                type="button"
                onClick={onToggle}
                aria-expanded={isExpanded}
                className="flex w-full items-baseline gap-2 py-3 text-left cursor-pointer group"
            >
                <i
                    className={cn(
                        "fa-sharp fa-solid fa-chevron-right text-[10px] text-secondary transition-transform mt-[3px]",
                        isExpanded && "rotate-90"
                    )}
                />
                <span className="text-[15px] text-primary group-hover:text-accent transition-colors">
                    {entryLabel(entry)}
                </span>
                {isCurrent && (
                    <span className="rounded-full bg-accent/20 text-accent text-[10px] px-2 py-[1px] leading-4">
                        current
                    </span>
                )}
                {headline && <span className="text-[13px] text-secondary truncate flex-1">{headline}</span>}
                <span className="ml-auto text-[12px] text-secondary whitespace-nowrap pl-2">
                    {formatDate(entry.publishedat)}
                </span>
            </button>
            {isExpanded && (
                <div className="pb-4 pl-[18px]">
                    {entry.body ? (
                        <Markdown
                            text={entry.body}
                            scrollable={false}
                            className="h-auto! overflow-visible!"
                            contentClassName="h-auto! overflow-visible!"
                            fontSizeOverride={13}
                        />
                    ) : (
                        <div className="text-[13px] text-secondary">No release notes.</div>
                    )}
                    <a
                        href={entry.url}
                        onClick={(e) => {
                            e.preventDefault();
                            openLink(entry.url);
                        }}
                        className="inline-block mt-3 text-[12px] text-accent hover:underline cursor-pointer"
                    >
                        View on GitHub <i className="fa-sharp fa-light fa-arrow-up-right-from-square ml-1" />
                    </a>
                </div>
            )}
        </div>
    );
};

interface ChangelogModalVProps {
    result: ChangelogResult;
    loading: boolean;
    version: string;
    versionString: string;
    onRefresh: () => void;
    onShowAbout: () => void;
    onClose: () => void;
}

const ChangelogModalV = ({
    result,
    loading,
    version,
    versionString,
    onRefresh,
    onShowAbout,
    onClose,
}: ChangelogModalVProps) => {
    const entries = result?.entries ?? [];
    const [expandedTags, setExpandedTags] = useState<{ [tag: string]: boolean }>(null);

    useEffect(() => {
        if (expandedTags != null || entries.length == 0) {
            return;
        }
        const current = entries.find((entry) => tagVersion(entry.tag) == version) ?? entries[0];
        setExpandedTags({ [current.tag]: true });
    }, [entries, expandedTags, version]);

    const toggle = (tag: string) => {
        setExpandedTags((prev) => ({ ...prev, [tag]: !prev?.[tag] }));
    };

    return (
        <Modal className="w-[640px] max-w-[92vw] pt-[26px] pb-4" onClose={onClose}>
            <div className="flex flex-col w-full min-h-0">
                <div className="flex items-baseline gap-3 pb-3 border-b border-border">
                    <div className="text-[18px] text-primary">What's New</div>
                    <a
                        href={ReleasesUrl}
                        onClick={(e) => {
                            e.preventDefault();
                            openLink(ReleasesUrl);
                        }}
                        className="ml-auto mr-8 text-[12px] text-secondary hover:text-accent transition-colors cursor-pointer"
                    >
                        <i className="fa-brands fa-github mr-1" />
                        All releases
                    </a>
                </div>
                {result?.stale && entries.length > 0 && (
                    <div className="flex items-center gap-2 mt-3 text-[12px] text-secondary">
                        <i className="fa-sharp fa-light fa-cloud-slash" />
                        Showing a cached copy — GitHub could not be reached.
                        <button
                            type="button"
                            onClick={onRefresh}
                            disabled={loading}
                            className="text-accent hover:underline cursor-pointer"
                        >
                            Retry
                        </button>
                    </div>
                )}
                <OverlayScrollbarsComponent
                    className="max-h-[60vh] min-h-[120px] overflow-y-auto"
                    options={{ scrollbars: { autoHide: "leave" } }}
                >
                    {entries.length > 0 ? (
                        entries.map((entry) => (
                            <ChangelogEntryRow
                                key={entry.tag}
                                entry={entry}
                                isCurrent={tagVersion(entry.tag) == version}
                                isExpanded={!!expandedTags?.[entry.tag]}
                                onToggle={() => toggle(entry.tag)}
                            />
                        ))
                    ) : (
                        <div className="flex flex-col items-center justify-center gap-3 py-10 text-secondary text-[13px]">
                            {loading ? (
                                <>
                                    <i className="fa-sharp fa-light fa-spinner-third fa-spin text-[18px]" />
                                    Loading changelog…
                                </>
                            ) : (
                                <>
                                    <i className="fa-sharp fa-light fa-cloud-slash text-[18px]" />
                                    <div className="text-center">
                                        Could not load the changelog.
                                        {result?.error && <div className="mt-1 opacity-70">{result.error}</div>}
                                    </div>
                                    <button
                                        type="button"
                                        onClick={onRefresh}
                                        className="bg-accent/80 text-primary rounded px-3 py-1 hover:bg-accent transition-colors cursor-pointer"
                                    >
                                        Try again
                                    </button>
                                </>
                            )}
                        </div>
                    )}
                </OverlayScrollbarsComponent>
                <div className="flex items-center gap-2 pt-3 mt-1 border-t border-border text-[12px] text-secondary">
                    <span>{versionString}</span>
                    <button
                        type="button"
                        onClick={onShowAbout}
                        className="ml-auto hover:text-accent transition-colors cursor-pointer"
                    >
                        About Wave
                    </button>
                </div>
            </div>
        </Modal>
    );
};

ChangelogModalV.displayName = "ChangelogModalV";

const ChangelogModal = () => {
    const fullConfig = useAtomValue(atoms.fullConfigAtom);
    const version = fullConfig?.version ?? "";
    const versionString = `${version} (${isDev() ? "dev-" : ""}${fullConfig?.buildtime ?? ""})`;
    const [result, setResult] = useState<ChangelogResult>(null);
    const [loading, setLoading] = useState(true);

    const load = useCallback((force: boolean) => {
        setLoading(true);
        fireAndForget(async () => {
            try {
                setResult(await getApi().getChangelog(force));
            } catch (e) {
                setResult({ entries: [], fetchedat: 0, stale: true, error: e.message ?? String(e) });
            } finally {
                setLoading(false);
            }
        });
    }, []);

    useEffect(() => {
        load(false);
    }, [load]);

    return (
        <ChangelogModalV
            result={result}
            loading={loading}
            version={version}
            versionString={versionString}
            onRefresh={() => load(true)}
            onShowAbout={() => modalsModel.pushModal("AboutModal")}
            onClose={() => modalsModel.popModal()}
        />
    );
};

ChangelogModal.displayName = "ChangelogModal";

export { ChangelogModal, ChangelogModalV };
