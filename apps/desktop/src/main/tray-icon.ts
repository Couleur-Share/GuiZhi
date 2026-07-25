import path from "path";

const MAC_TRAY_TEMPLATE_FILENAME = "GuiZhiStatusTemplate.png";

interface MacTrayIconPathOptions {
  dirname: string;
  isDev: boolean;
  resourcesPath: string;
}

interface TemplateNativeImage {
  isEmpty(): boolean;
  setTemplateImage(isTemplate: boolean): void;
}

interface LoadMacTrayTemplateIconOptions<TImage extends TemplateNativeImage> {
  createFromPath(filePath: string): TImage;
  templatePath: string;
}

export function resolveMacTrayIconPaths({
  dirname,
  isDev,
  resourcesPath,
}: MacTrayIconPathOptions): {
  fallbackPath: string;
  templatePath: string;
} {
  const resourceRoot = isDev
    ? path.join(dirname, "../../resources")
    : resourcesPath;

  return {
    fallbackPath: path.join(resourceRoot, "icon.iconset/icon_16x16@2x.png"),
    templatePath: path.join(resourceRoot, "tray", MAC_TRAY_TEMPLATE_FILENAME),
  };
}

export function loadMacTrayTemplateIcon<TImage extends TemplateNativeImage>({
  createFromPath,
  templatePath,
}: LoadMacTrayTemplateIconOptions<TImage>): TImage {
  const icon = createFromPath(templatePath);
  if (icon.isEmpty()) {
    throw new Error(`macOS tray template icon is missing: ${templatePath}`);
  }

  icon.setTemplateImage(true);
  return icon;
}
