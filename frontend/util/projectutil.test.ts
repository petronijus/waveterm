// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from "vitest";
import { projectNameFromPath, tildifyPath, uniqueProjectName } from "./projectutil";

describe("projectNameFromPath", () => {
    it("uses the folder basename", () => {
        expect(projectNameFromPath("/home/user/Documents/Dev/myproj")).toBe("myproj");
        expect(projectNameFromPath("~/Documents/Dev/myproj")).toBe("myproj");
    });
    it("handles trailing slashes and windows separators", () => {
        expect(projectNameFromPath("/home/user/proj/")).toBe("proj");
        expect(projectNameFromPath("C:\\Users\\user\\proj")).toBe("proj");
    });
    it("falls back to 'home' for ~ and blank", () => {
        expect(projectNameFromPath("~")).toBe("home");
        expect(projectNameFromPath("")).toBe("home");
        expect(projectNameFromPath(null)).toBe("home");
    });
});

describe("uniqueProjectName", () => {
    it("returns the base name when free", () => {
        expect(uniqueProjectName("proj", {})).toBe("proj");
    });
    it("suffixes a counter on collisions", () => {
        const projects = { proj: { path: "~/a" }, "proj (2)": { path: "~/b" } } as {
            [key: string]: ProjectConfigType;
        };
        expect(uniqueProjectName("proj", projects)).toBe("proj (3)");
    });
});

describe("tildifyPath", () => {
    const home = "/home/user";
    it("tildifies paths under home", () => {
        expect(tildifyPath("/home/user/Documents/Dev/proj", home)).toBe("~/Documents/Dev/proj");
    });
    it("tildifies home itself", () => {
        expect(tildifyPath("/home/user", home)).toBe("~");
        expect(tildifyPath("/home/user/", home)).toBe("~");
    });
    it("leaves paths outside home alone", () => {
        expect(tildifyPath("/opt/data", home)).toBe("/opt/data");
        expect(tildifyPath("/home/userx/proj", home)).toBe("/home/userx/proj");
    });
    it("leaves already-tildified paths alone", () => {
        expect(tildifyPath("~/Documents", home)).toBe("~/Documents");
        expect(tildifyPath("~", home)).toBe("~");
    });
    it("passes through when home is unknown", () => {
        expect(tildifyPath("/home/user/proj", "")).toBe("/home/user/proj");
        expect(tildifyPath("/home/user/proj", null)).toBe("/home/user/proj");
    });
    it("normalizes windows separators against a windows home", () => {
        expect(tildifyPath("C:\\Users\\user\\proj", "C:\\Users\\user")).toBe("~/proj");
        expect(tildifyPath("C:/Users/user/proj", "C:\\Users\\user")).toBe("~/proj");
    });
});
