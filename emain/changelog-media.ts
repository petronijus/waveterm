// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// GitHub writes an uploaded image into a release body as markdown (![](url)) but leaves an
// uploaded video as a bare attachment URL, and that URL carries no extension -- these helpers
// find the bare URLs and, once their kind is known, turn them into something that renders.

const BareUrlRe = /(?<![(<"'=])https?:\/\/[^\s<>)"']+/g;
// fenced blocks, code spans and link reference definitions are markdown that happens to contain
// a URL -- rewriting inside them would corrupt a code sample or break a reference
const ProtectedRe = /```[\s\S]*?```|`[^`\n]*`|^[ \t]*\[[^\]\n]+\]:[ \t]*\S+$/gm;
const AttachmentUrlRe = /^https:\/\/github\.com\/user-attachments\/assets\/[0-9a-fA-F-]+$/;
const ImageExtRe = /\.(?:png|jpe?g|gif|webp|avif|svg)(?:\?[^\s]*)?$/i;
const VideoExtRe = /\.(?:mp4|webm|mov|m4v)(?:\?[^\s]*)?$/i;

const MediaKindImage = "image";
const MediaKindVideo = "video";

function mapOutsideProtected(text: string, mapFn: (segment: string) => string): string {
    let out = "";
    let last = 0;
    for (const match of text.matchAll(ProtectedRe)) {
        out += mapFn(text.slice(last, match.index)) + match[0];
        last = match.index + match[0].length;
    }
    return out + mapFn(text.slice(last));
}

export function collectBareUrls(body: string): string[] {
    const urls: string[] = [];
    mapOutsideProtected(body, (segment) => {
        for (const url of segment.match(BareUrlRe) ?? []) {
            urls.push(url);
        }
        return segment;
    });
    return urls;
}

export function isAttachmentUrl(url: string): boolean {
    return AttachmentUrlRe.test(url);
}

export function mediaKindFromUrl(url: string): string {
    if (ImageExtRe.test(url)) {
        return MediaKindImage;
    }
    if (VideoExtRe.test(url)) {
        return MediaKindVideo;
    }
    return "";
}

export function mediaKindFromContentType(contentType: string): string {
    if (contentType.startsWith("image/")) {
        return MediaKindImage;
    }
    if (contentType.startsWith("video/")) {
        return MediaKindVideo;
    }
    return "";
}

export function embedBareMedia(body: string, kinds: Map<string, string>): string {
    return mapOutsideProtected(body, (segment) =>
        segment.replace(BareUrlRe, (url) => {
            const kind = kinds.get(url);
            if (kind == MediaKindImage) {
                return `![](${url})`;
            }
            if (kind == MediaKindVideo) {
                return `<video src="${url}" controls></video>`;
            }
            return url;
        })
    );
}
