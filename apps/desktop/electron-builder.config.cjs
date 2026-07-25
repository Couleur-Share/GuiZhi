const enableMacReleaseSigning =
  process.env.GUIZHI_MAC_RELEASE_SIGN === "true";

const macReleaseSigningConfig = enableMacReleaseSigning
  ? {
      hardenedRuntime: true,
      notarize: true,
      entitlements: "resources/entitlements.mac.plist",
      entitlementsInherit: "resources/entitlements.mac.inherit.plist",
    }
  : {
      hardenedRuntime: false,
      identity: null,
    };

module.exports = {
  appId: "com.couleurshare.guizhi",
  productName: "GuiZhi",
  directories: {
    output: "dist",
    buildResources: "resources",
  },
  extraMetadata: {
    main: "out/main/index.js",
  },
  files: ["out/**/*"],
  extraResources: [
    {
      from: "resources/icon.ico",
      to: "icon.ico",
    },
    {
      from: "resources/icon.png",
      to: "icon.png",
    },
    // 注意：macOS 图标资产（icon.icns / icon.iconset / tray 模板图案）仍是
    // PromptHub 图案；恢复 mac 发布矩阵前必须替换为归知图标（Windows 的
    // icon.ico / icon.png 已是归知图标）。
    {
      from: "resources/icon.iconset",
      to: "icon.iconset",
    },
    {
      from: "resources/tray",
      to: "tray",
    },
    {
      from: "../../CHANGELOG.md",
      to: "CHANGELOG.md",
    },
  ],
  asarUnpack: ["**/*.node", "**/node-sqlite3-wasm/**"],
  asar: true,
  npmRebuild: false,
  nodeGypRebuild: false,
  mac: {
    target: [
      {
        target: "dmg",
        arch: ["x64", "arm64"],
      },
      {
        target: "zip",
        arch: ["x64", "arm64"],
      },
    ],
    artifactName: "${productName}-${version}-${arch}.${ext}",
    icon: "resources/icon.icns",
    category: "public.app-category.productivity",
    gatekeeperAssess: false,
    ...macReleaseSigningConfig,
  },
  win: {
    target: [
      {
        target: "nsis",
        arch: ["x64", "arm64"],
      },
    ],
    artifactName: "${productName}-Setup-${version}-${arch}.${ext}",
    icon: "resources/icon.ico",
  },
  linux: {
    target: [
      {
        target: "AppImage",
        arch: ["x64"],
      },
      {
        target: "deb",
        arch: ["x64"],
      },
    ],
    category: "Utility",
    artifactName: "${productName}-${version}-${arch}.${ext}",
  },
  appImage: {
    artifactName: "${productName}-${version}-x64.${ext}",
  },
  nsis: {
    guid: "fa5aa350-68bd-43b9-b440-a7ecb8fcc78a",
    oneClick: false,
    allowToChangeInstallationDirectory: true,
    createDesktopShortcut: true,
    createStartMenuShortcut: true,
    differentialPackage: false,
    include: "resources/installer.nsh",
  },
  publish: {
    provider: "github",
    owner: "Couleur-Share",
    repo: "GuiZhi",
    releaseType: "release",
  },
  releaseInfo: {
    releaseNotesFile: "../../CHANGELOG.md",
  },
};
