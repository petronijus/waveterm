/**
 * Script to get the current package version and bump the version, if specified.
 *
 * If no arguments are present, the current version will returned.
 * If only a single argument is given, the following are valid inputs:
 *      - `none`: No-op.
 *      - `patch`: Bumps the patch version.
 *      - `minor`: Bumps the minor version.
 *      - `major`: Bumps the major version.
 *      - '1', 'true': Bumps the prerelease version.
 * If two arguments are given, the following are valid inputs for the first argument:
 *      - `none`: No-op.
 *      - `patch`: Bumps the patch version.
 *      - `minor`: Bumps the minor version.
 *      - `major`: Bumps the major version.
 * The following are valid inputs for the second argument:
 *      - `0`, 'false': The release is not a prerelease, will remove any prerelease identifier from the version, if one was present.
 *      - '1', 'true': The release is a prerelease (any value other than `0` or `false` will be interpreted as `true`).
 */

const path = require("path");
const packageJsonPath = path.resolve(__dirname, "package.json");
const packageJson = require(packageJsonPath);

const VERSION = `${packageJson.version}`;
module.exports = VERSION;

if (typeof require !== "undefined" && require.main === module) {
    if (process.argv.length > 2) {
        const fs = require("fs");
        const semver = require("semver");

        let action = process.argv[2];

        // If prerelease argument is not explicitly set, mark it as undefined.
        const isPrerelease =
            process.argv.length > 3
                ? process.argv[3] !== "false" && process.argv[3] !== "0"
                : action === "true" || action === "1"
                  ? true
                  : undefined;

        // This will remove the prerelease version string (i.e. 0.1.13-beta.1 -> 0.1.13) if the arguments are `none 0` and the current version is a prerelease.
        if (action === "none" && isPrerelease === false && semver.prerelease(VERSION)) {
            action = "patch";
        }

        let newVersion = packageJson.version;
        switch (action) {
            case "major":
            case "minor":
            case "patch":
                newVersion = semver.inc(
                    VERSION,
                    `${isPrerelease ? "pre" : ""}${action}`,
                    null,
                    isPrerelease ? "beta" : null
                );
                break;
            case "none":
            case "true":
            case "1":
                if (isPrerelease) newVersion = semver.inc(VERSION, "prerelease", null, "beta");
                break;
            case "pj": {
                // Fork iteration on top of the upstream base it was cut from: base 0.14.5
                // stays 0.14.5 and gains `-pj.N`. Note semver ranks `0.14.5-pj.N` BELOW a
                // plain `0.14.5`, so a build that reports the bare base version will never
                // be offered `-pj.N` as an update — that only ever affected the pre-pj.11
                // builds, which all shipped reporting `0.14.5`, and they need a manual
                // reinstall regardless (unsigned -> Developer ID signed).
                const base = `${semver.major(VERSION)}.${semver.minor(VERSION)}.${semver.patch(VERSION)}`;
                const pre = semver.prerelease(VERSION);
                const pkgN = pre && pre[0] === "pj" ? Number(pre[1]) : 0;
                // The counter is global across bases (0.14.5-pj.11 -> 0.14.6-pj.12, no reset).
                // An upstream merge overwrites package.json with a bare base version, so the
                // last used N is recovered from the release tags, not just the prerelease field.
                let tagN = 0;
                try {
                    const tags = require("child_process").execSync("git tag --list 'v*-pj.*'", {
                        cwd: __dirname,
                        encoding: "utf8",
                    });
                    for (const match of tags.matchAll(/-pj\.(\d+)$/gm)) {
                        tagN = Math.max(tagN, Number(match[1]));
                    }
                } catch {
                    // not a git checkout or git unavailable — package.json is the best we have
                }
                newVersion = `${base}-pj.${Math.max(pkgN, tagN) + 1}`;
                break;
            }
            default:
                throw new Error(`Unknown action ${action}`);
        }
        packageJson.version = newVersion;
        fs.writeFileSync(packageJsonPath, JSON.stringify(packageJson, null, 4) + "\n");
        console.log(newVersion);
    } else {
        console.log(VERSION);
    }
}
