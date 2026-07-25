import React from "react";
import ReactDOM from "react-dom/client";
import { ToastProvider } from "./components/ui/Toast";
import "./styles/globals.css";
import { i18nReady } from "./i18n";

// Start loading the app while the selected locale is initialized, without
// making the renderer entry parse every application feature before it can boot.
const appModule = import("./App");
const App = React.lazy(() => appModule);

void i18nReady.then(() => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <ToastProvider>
        <React.Suspense
          fallback={<div className="h-screen bg-background" aria-busy="true" />}
        >
          <App />
        </React.Suspense>
      </ToastProvider>
    </React.StrictMode>,
  );
});
