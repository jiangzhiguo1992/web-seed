# P3 完善：静态构建 + nginx/unprivileged + SPA fallback
# 注意：node:26-alpine / nginx:1.27-alpine tag 需在 P3 用真实 Docker 验证（见 architecture.md A5）。
FROM node:26-alpine AS build
WORKDIR /src
RUN corepack enable
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY apps/web/admin/package.json apps/web/admin/
COPY apps/web/packages ./apps/web/packages
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm -r build

FROM nginx:1.27-alpine
COPY --from=build /src/apps/web/admin/dist /usr/share/nginx/html
EXPOSE 80
