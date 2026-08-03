const { Arch } = require("electron-builder");
const pkg = require("./package.json");
const fs = require("fs");
const path = require("path");

const windowsShouldSign = !!process.env.SM_CODE_SIGNING_CERT_SHA1_HASH;

// pj fork: mac signing/notarization is opt-in via env so ordinary `task dev` / local
// packaging keeps working with no Apple credentials. Squirrel.Mac refuses to apply an
// update whose new bundle isn't signed with the same Developer ID as the running app,
// so an unsigned build can download an update but never install it.
const macShouldNotarize = !!process.env.APPLE_TEAM_ID && !!process.env.APPLE_ID;

/**
 * @type {import('electron-builder').Configuration}
 * @see https://www.electron.build/configuration/configuration
 */
const config = {
    appId: pkg.build.appId,
    productName: pkg.productName,
    executableName: pkg.productName,
    artifactName: "${productName}-${platform}-${arch}-${version}.${ext}",
    generateUpdatesFilesForAllChannels: true,
    npmRebuild: false,
    nodeGypRebuild: false,
    electronCompile: false,
    files: [
        {
            from: "./dist",
            to: "./dist",
            filter: ["**/*", "!bin/*", "bin/wavesrv.${arch}*", "bin/wsh*", "!tsunamiscaffold/**/*"],
        },
        {
            from: ".",
            to: ".",
            filter: ["package.json"],
        },
        "!node_modules", // We don't need electron-builder to package in Node modules as Vite has already bundled any code that our program is using.
    ],
    extraResources: [
        {
            from: "dist/tsunamiscaffold",
            to: "tsunamiscaffold",
        },
    ],
    directories: {
        output: "make",
    },
    asarUnpack: [
        "dist/bin/**/*", // wavesrv and wsh binaries
        "dist/schema/**/*", // schema files for Monaco editor
    ],
    mac: {
        target: [
            {
                target: "zip",
                arch: ["arm64", "x64"],
            },
            {
                target: "dmg",
                arch: ["arm64", "x64"],
            },
        ],
        category: "public.app-category.developer-tools",
        minimumSystemVersion: "10.15.0",
        // codesign defaults to plain-HTTP timestamp.apple.com, which transparent
        // HTTP proxies (captive portals, train wifi) corrupt — "A timestamp was
        // expected but was not found" kills the build hundreds of files in. The
        // https endpoint bypasses the proxy and works everywhere.
        timestamp: "https://timestamp.apple.com/ts01",
        notarize: macShouldNotarize && { teamId: process.env.APPLE_TEAM_ID },
        mergeASARs: true,
        singleArchFiles: "**/dist/bin/wavesrv.*",
        entitlements: "build/entitlements.mac.plist",
        entitlementsInherit: "build/entitlements.mac.plist",
        extendInfo: {
            NSContactsUsageDescription: "A CLI application running in Wave wants to use your contacts.",
            NSRemindersUsageDescription: "A CLI application running in Wave wants to use your reminders.",
            NSLocationWhenInUseUsageDescription:
                "A CLI application running in Wave wants to use your location information while active.",
            NSLocationAlwaysUsageDescription:
                "A CLI application running in Wave wants to use your location information, even in the background.",
            NSCameraUsageDescription: "A CLI application running in Wave wants to use the camera.",
            NSMicrophoneUsageDescription: "A CLI application running in Wave wants to use your microphone.",
            NSCalendarsUsageDescription: "A CLI application running in Wave wants to use Calendar data.",
            NSLocationUsageDescription: "A CLI application running in Wave wants to use your location information.",
            NSAppleEventsUsageDescription: "A CLI application running in Wave wants to use AppleScript.",
        },
    },
    linux: {
        artifactName: "${name}-${platform}-${arch}-${version}.${ext}",
        category: "TerminalEmulator",
        executableName: pkg.name,
        target: ["zip", "deb", "rpm", "snap", "AppImage", "pacman"],
        synopsis: pkg.description,
        description: null,
        desktop: {
            entry: {
                Name: pkg.productName,
                Comment: pkg.description,
                Keywords: "developer;terminal;emulator;",
                Categories: "Development;Utility;",
            },
        },
        executableArgs: ["--enable-features", "UseOzonePlatform", "--ozone-platform-hint", "auto"], // Hint Electron to use Ozone abstraction layer for native Wayland support
    },
    deb: {
        afterInstall: "build/deb-postinstall.tpl",
    },
    win: {
        target: ["nsis", "msi", "zip"],
        signtoolOptions: windowsShouldSign && {
            signingHashAlgorithms: ["sha256"],
            publisherName: "Command Line Inc",
            certificateSubjectName: "Command Line Inc",
            certificateSha1: process.env.SM_CODE_SIGNING_CERT_SHA1_HASH,
        },
    },
    appImage: {
        license: "LICENSE",
    },
    snap: {
        base: "core22",
        confinement: "classic",
        allowNativeWayland: true,
        artifactName: "${name}_${version}_${arch}.${ext}",
    },
    rpm: {
        // this should remove /usr/lib/.build-id/ links which can conflict with other electron apps like slack
        fpm: ["--rpm-rpmbuild-define", "_build_id_links none"],
    },
    // pj fork: point auto-update at THIS fork's own GitHub releases, never the official
    // Wave server (dl.waveterm.dev). The official feed would hand the fork a stock-Wave
    // build and silently overwrite the fork's frontend (lost sync button / activity
    // indicator / theme work). With the feed scoped to petronijus/waveterm the updater
    // can only ever offer fork builds — upstream changes still reach the fork through git
    // merges + a fork release, so we always run the fork and never revert to stock.
    // The channel MUST match the `pj` prerelease identifier in the version. At runtime
    // GitHubProvider re-derives the channel from the release TAG, and only accepts a
    // release whose tag channel equals the updater's channel (or is alpha/beta). Leaving
    // this unset bakes no channel into app-update.yml, the updater falls back to "latest",
    // and every `-pj.N` tag then fails to match -> ERR_UPDATER_NO_PUBLISHED_VERSIONS.
    publish: {
        provider: "github",
        owner: "petronijus",
        repo: "waveterm",
        channel: "pj",
    },
    afterPack: (context) => {
        // This is a workaround to restore file permissions to the wavesrv binaries on macOS after packaging the universal binary.
        if (context.electronPlatformName === "darwin" && context.arch === Arch.universal) {
            const packageBinDir = path.resolve(
                context.appOutDir,
                `${pkg.productName}.app/Contents/Resources/app.asar.unpacked/dist/bin`
            );

            // Reapply file permissions to the wavesrv binaries in the final app package
            fs.readdirSync(packageBinDir, {
                recursive: true,
                withFileTypes: true,
            })
                .filter((f) => f.isFile() && f.name.startsWith("wavesrv"))
                .forEach((f) => fs.chmodSync(path.resolve(f.parentPath ?? f.path, f.name), 0o755)); // 0o755 corresponds to -rwxr-xr-x
        }
    },
};

module.exports = config;
