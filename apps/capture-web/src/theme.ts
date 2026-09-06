import { useEffect, useLayoutEffect, useState } from "react";

export type Theme = "auto" | "light" | "dark";
const systemTheme = matchMedia("(prefers-color-scheme: dark)");
function readTheme(): Theme {
  const saved = localStorage.getItem("theme");
  return saved === "light" || saved === "dark" ? saved : "auto";
}
function applyTheme(theme: Theme, systemDark: boolean) {
  const resolved = theme === "auto" ? (systemDark ? "dark" : "light") : theme;
  document.documentElement.dataset.theme = resolved;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", resolved === "dark" ? "#0d1016" : "#f3f4f6");
}
// 在首次渲染前应用已保存的外观，避免深色用户先看到浅色页面。
applyTheme(readTheme(), systemTheme.matches);

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [systemDark, setSystemDark] = useState(systemTheme.matches);
  useEffect(() => {
    const update = () => setSystemDark(systemTheme.matches);
    systemTheme.addEventListener("change", update);
    return () => systemTheme.removeEventListener("change", update);
  }, []);
  useLayoutEffect(() => {
    applyTheme(theme, systemDark); localStorage.setItem("theme", theme);
  }, [theme, systemDark]);
  return { theme, setTheme, dark: theme === "dark" || (theme === "auto" && systemDark) };
}
