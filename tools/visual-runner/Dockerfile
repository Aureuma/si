FROM node:22-bullseye

WORKDIR /runner

COPY package.json package-lock.json ./
RUN npm config set fetch-retries 5 \
 && npm config set fetch-retry-factor 2 \
 && npm config set fetch-timeout 120000 \
 && npm config set cache /tmp/npm-cache --global \
 && npm cache clean --force \
 && npm ci --registry=https://registry.npmjs.org --ignore-scripts=false --foreground-scripts
RUN npx playwright install --with-deps chromium

COPY tests ./tests

ENTRYPOINT ["npm", "test"]
