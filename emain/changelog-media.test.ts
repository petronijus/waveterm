// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import {
    collectBareUrls,
    embedBareMedia,
    isAttachmentUrl,
    mediaKindFromContentType,
    mediaKindFromUrl,
} from "./changelog-media";

const AttachmentUrl = "https://github.com/user-attachments/assets/2b0b9d1e-5f2a-4f39-9a0f-9c8f2f3a1111";

function embed(body: string, kinds: [string, string][]): string {
    return embedBareMedia(body, new Map(kinds));
}

describe("collectBareUrls", () => {
    it("finds a bare URL on its own line", () => {
        expect(collectBareUrls(`see the demo\n\n${AttachmentUrl}\n`)).toEqual([AttachmentUrl]);
    });

    it("skips URLs that markdown already owns", () => {
        const body = `![shot](${AttachmentUrl})\n[link](https://example.com/a.png)\n<https://example.com/b.png>\n<video src="https://example.com/c.mp4" controls></video>`;
        expect(collectBareUrls(body)).toEqual([]);
    });

    it("skips fenced blocks, code spans and reference definitions", () => {
        const body = [
            "```sh",
            "curl https://example.com/inside-fence.png",
            "```",
            "`https://example.com/inside-span.mp4`",
            "[ref]: https://example.com/reference.png",
            "https://example.com/real.png",
        ].join("\n");
        expect(collectBareUrls(body)).toEqual(["https://example.com/real.png"]);
    });
});

describe("mediaKindFromUrl", () => {
    it("reads the extension, query string and all", () => {
        expect(mediaKindFromUrl("https://example.com/a.GIF")).toBe("image");
        expect(mediaKindFromUrl("https://example.com/a.mp4?raw=1")).toBe("video");
        expect(mediaKindFromUrl("https://example.com/release")).toBe("");
    });
});

describe("isAttachmentUrl", () => {
    it("matches only GitHub's own attachment assets", () => {
        expect(isAttachmentUrl(AttachmentUrl)).toBe(true);
        expect(isAttachmentUrl("https://example.com/user-attachments/assets/abc")).toBe(false);
    });
});

describe("mediaKindFromContentType", () => {
    it("maps the content type onto a kind", () => {
        expect(mediaKindFromContentType("image/gif")).toBe("image");
        expect(mediaKindFromContentType("video/mp4")).toBe("video");
        expect(mediaKindFromContentType("application/octet-stream")).toBe("");
    });
});

describe("embedBareMedia", () => {
    it("turns a probed image into a markdown image and a video into a player", () => {
        const body = `before\n${AttachmentUrl}\nhttps://example.com/clip.mp4\nafter`;
        const out = embed(body, [
            [AttachmentUrl, "image"],
            ["https://example.com/clip.mp4", "video"],
        ]);
        expect(out).toBe(
            `before\n![](${AttachmentUrl})\n<video src="https://example.com/clip.mp4" controls></video>\nafter`
        );
    });

    it("leaves a URL of unknown kind as a plain link", () => {
        const body = "https://example.com/release-notes";
        expect(embed(body, [["https://example.com/release-notes", ""]])).toBe(body);
    });

    it("never rewrites inside a fenced block", () => {
        const body = "```\nhttps://example.com/a.png\n```\nhttps://example.com/a.png";
        expect(embed(body, [["https://example.com/a.png", "image"]])).toBe(
            "```\nhttps://example.com/a.png\n```\n![](https://example.com/a.png)"
        );
    });
});
