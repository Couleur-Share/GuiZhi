FROM node:24.15.0-bookworm-slim@sha256:152aceace5c03e2597988763165ee33e3fd3633636db0fc983cd2e126b02cfde AS build
WORKDIR /source
RUN npm install --global pnpm@11.1.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY packages/shared packages/shared
COPY apps/capture-web apps/capture-web
COPY apps/capture-relay apps/capture-relay
RUN pnpm install --filter @guizhi/capture-web... --filter @guizhi/capture-relay... --frozen-lockfile
RUN pnpm --filter @guizhi/capture-web build && pnpm --filter @guizhi/capture-relay build
RUN pnpm --filter @guizhi/capture-relay deploy --legacy --prod /runtime

FROM node:24.15.0-bookworm-slim@sha256:152aceace5c03e2597988763165ee33e3fd3633636db0fc983cd2e126b02cfde
WORKDIR /app
COPY --from=build /runtime/ /app/
COPY --from=build /source/apps/capture-relay/dist/ /app/dist/
COPY --from=build /source/apps/capture-web/dist/ /app/web/
ENV NODE_ENV=production CAPTURE_DATABASE=/data/capture.db CAPTURE_WEB_ROOT=/app/web CAPTURE_BACKUPS=/backups PORT=8787
USER node
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD node -e "fetch('http://127.0.0.1:8787/healthz').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"
CMD ["node", "dist/main.js"]
