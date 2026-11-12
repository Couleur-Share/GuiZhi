FROM node:24.22.3-bookworm-slim@sha256:dc26ae92c9fb15cf6b1d63e20f0944b2efe0f115b20e00be48fb23fceb061c54 AS build
WORKDIR /source
RUN npm install --global pnpm@11.1.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY packages/shared packages/shared
COPY apps/capture-web apps/capture-web
COPY apps/capture-relay apps/capture-relay
RUN pnpm install --filter @guizhi/capture-web... --filter @guizhi/capture-relay... --frozen-lockfile
RUN pnpm --filter @guizhi/capture-web build && pnpm --filter @guizhi/capture-relay build
RUN pnpm --filter @guizhi/capture-relay deploy --legacy --prod /runtime

FROM node:24.22.3-bookworm-slim@sha256:dc26ae92c9fb15cf6b1d63e20f0944b2efe0f115b20e00be48fb23fceb061c54
WORKDIR /app
COPY --from=build /runtime/ /app/
COPY --from=build /source/apps/capture-relay/dist/ /app/dist/
COPY --from=build /source/apps/capture-web/dist/ /app/web/
ENV NODE_ENV=production CAPTURE_DATABASE=/data/capture.db CAPTURE_WEB_ROOT=/app/web CAPTURE_BACKUPS=/backups PORT=8787
USER node
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD node -e "fetch('http://127.0.0.1:8787/healthz').then(r=>{if(!r.ok)process.exit(1)}).catch(()=>process.exit(1))"
CMD ["node", "dist/main.js"]
