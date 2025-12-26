# syntax=docker/dockerfile:1
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

COPY --from=build /repo/tools/app-entrypoint.sh /entrypoint.sh
COPY --from=build /repo/${APP_PATH}/build ./build
COPY --from=build /repo/${APP_PATH}/package.json ./package.json

RUN chmod +x /entrypoint.sh
EXPOSE 3000
ENTRYPOINT ["/entrypoint.sh"]
CMD ["node", "build"]
