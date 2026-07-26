import React from "react";
import ReactDOM from "react-dom/client";
import { ErrorBoundary } from "./components/ui/ErrorBoundary";
import { ToastProvider } from "./components/ui/Toast";
import { TooltipLayer } from "./components/ui/TooltipLayer";
import "./styles/globals.css";
import { i18nReady } from "./i18n";

// Start loading the app while the selected locale is initialized, without
// making the renderer entry parse every application feature before it can boot.
const appModule = import("./App");
const App = React.lazy(() => appModule);

// i18n 起不来也要照常挂载：在这里静默 return 的结果是一整块纯色，
// 而交给 ErrorBoundary 至少能把原因显示出来。
void i18nReady
  .catch((error: unknown) => {
    console.error("i18n 初始化失败，仍继续渲染：", error);
  })
  .then(() => {
    ReactDOM.createRoot(document.getElementById("root")!).render(
      <React.StrictMode>
        <ErrorBoundary>
          <ToastProvider>
            <React.Suspense
              fallback={
                <div className="h-screen bg-background" aria-busy="true" />
              }
            >
              <App />
            </React.Suspense>
            <TooltipLayer />
          </ToastProvider>
        </ErrorBoundary>
      </React.StrictMode>,
    );
  });
