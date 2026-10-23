phase=dependencies
if [[ "${PREFLIGHT_PREPARED:-}" != "1" ]]; then pnpm --filter @guizhi/capture-web... --filter @guizhi/capture-relay... install --frozen-lockfile; fi
phase=shared-typecheck
pnpm --filter @guizhi/shared typecheck
phase=capture-web-typecheck
pnpm --filter @guizhi/capture-web typecheck
phase=capture-relay-typecheck
pnpm --filter @guizhi/capture-relay typecheck
phase=capture-relay-tests
pnpm --filter @guizhi/capture-relay test
phase=capture-build
pnpm --filter @guizhi/capture-web build
pnpm --filter @guizhi/capture-relay build
phase=capture-runtime
node scripts/wslc/capture-smoke.mjs
phase=artifacts
tar -czf /output/build-artifacts.tar.gz apps/capture-web/dist apps/capture-relay/dist
