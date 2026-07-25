import { lazy, Suspense } from "react";
import { useUIStore } from "../../stores/ui.store";
import { Spinner } from "../ui/Spinner";

const LibraryWorkspace = lazy(() =>
  import("../library/LibraryWorkspace").then((module) => ({
    default: module.LibraryWorkspace,
  })),
);
const ImportsWorkspace = lazy(() =>
  import("../imports/ImportsWorkspace").then((module) => ({
    default: module.ImportsWorkspace,
  })),
);
const AskWorkspace = lazy(() =>
  import("../ask/AskWorkspace").then((module) => ({
    default: module.AskWorkspace,
  })),
);
const WikiWorkspace = lazy(() =>
  import("../wiki/WikiWorkspace").then((module) => ({
    default: module.WikiWorkspace,
  })),
);

const loadingFallback = (
  <div className="flex flex-1 items-center justify-center">
    <Spinner />
  </div>
);

export function MainContent() {
  const appModule = useUIStore((state) => state.appModule);

  if (appModule === "library") {
    return (
      <Suspense fallback={loadingFallback}>
        <LibraryWorkspace />
      </Suspense>
    );
  }

  if (appModule === "imports") {
    return (
      <Suspense fallback={loadingFallback}>
        <ImportsWorkspace />
      </Suspense>
    );
  }

  if (appModule === "ask") {
    return (
      <Suspense fallback={loadingFallback}>
        <AskWorkspace />
      </Suspense>
    );
  }

  return (
    <Suspense fallback={loadingFallback}>
      <WikiWorkspace />
    </Suspense>
  );
}
