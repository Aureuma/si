# syntax=docker/dockerfile:1
FROM golang:1.22-bookworm AS app-entrypoint-build
WORKDIR /src/tools/app-entrypoint
COPY tools/app-entrypoint/go.mod ./go.mod
COPY tools/app-entrypoint/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/app-entrypoint .

FROM node:22-bookworm AS build

WORKDIR /repo
ARG APP_PATH

COPY . .
RUN corepack enable
RUN if [ -f "${APP_PATH}/package-lock.json" ]; then \
      cd "${APP_PATH}" && npm ci; \
    elif [ -f "/repo/pnpm-lock.yaml" ]; then \
      pnpm install --frozen-lockfile; \
    else \
      pnpm install --frozen-lockfile=false; \
    fi
RUN if [ -f "${APP_PATH}/package-lock.json" ]; then \
      cd "${APP_PATH}" && npm run build; \
    else \
      pnpm -C "${APP_PATH}" run build; \
    fi

FROM node:22-bookworm AS runner
WORKDIR /app
ENV NODE_ENV=production

COPY --from=app-entrypoint-build /out/app-entrypoint /usr/local/bin/app-entrypoint
COPY --from=build /repo/${APP_PATH}/build ./build
COPY --from=build /repo/${APP_PATH}/package.json ./package.json

EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/app-entrypoint"]
CMD ["node", "build"]
