export function getRendererPlatform(): "darwin" | "win32" | "linux" {
  const userAgent = navigator.userAgent.toLowerCase();
  if (userAgent.includes("win")) return "win32";
  if (userAgent.includes("mac")) return "darwin";
  return "linux";
}
