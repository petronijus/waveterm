// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import backgroundsJson from "../../../pkg/wconfig/defaultconfig/backgrounds.json";
import mimetypesJson from "../../../pkg/wconfig/defaultconfig/mimetypes.json";
import presetsJson from "../../../pkg/wconfig/defaultconfig/presets.json";
import settingsJson from "../../../pkg/wconfig/defaultconfig/settings.json";
import flagsJson from "../../../pkg/wconfig/defaultconfig/flags.json";
import termthemesJson from "../../../pkg/wconfig/defaultconfig/termthemes.json";
import uithemesJson from "../../../pkg/wconfig/defaultconfig/uithemes.json";
import waveaiJson from "../../../pkg/wconfig/defaultconfig/waveai.json";
import widgetsJson from "../../../pkg/wconfig/defaultconfig/widgets.json";

export const DefaultFullConfig: FullConfigType = {
    settings: settingsJson as SettingsType,
    mimetypes: mimetypesJson as unknown as { [key: string]: MimeTypeConfigType },
    defaultwidgets: widgetsJson as unknown as { [key: string]: WidgetConfigType },
    widgets: {},
    presets: presetsJson as unknown as { [key: string]: MetaType },
    termthemes: termthemesJson as unknown as { [key: string]: TermThemeType },
    uithemes: uithemesJson as unknown as { [key: string]: UIThemeType },
    flags: flagsJson as unknown as { [key: string]: TabFlagType },
    connections: {},
    bookmarks: {},
    waveai: waveaiJson as unknown as { [key: string]: AIModeConfigType },
    backgrounds: backgroundsJson as { [key: string]: BackgroundConfigType },
    configerrors: [],
    version: "",
    buildtime: "",
};
