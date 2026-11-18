# The browser tests, with their dependencies installed, ready to run on the compose network.
# The image tag must match the @playwright/test version in package.json: the browsers in the
# image belong to exactly that release.
FROM mcr.microsoft.com/playwright:v1.65.1-noble@sha256:1507cbfc550a63a6b7396791ca4f00df17beb56342d2447b3f106c306f525579
WORKDIR /e2e
# Keep in step with "packageManager" in package.json.
RUN npm install --global pnpm@10.28.0
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
ENV CI=1
ENTRYPOINT ["pnpm", "exec", "playwright", "test"]
