# Development container (#664): the pinned Go toolchain, Node for the browser
# QA harness, and the Playwright browsers, in one step.
#
# No version is written in this file. install-toolchain.sh reads Go and
# golangci-lint from .github/workflows/ci.yml and Node from
# .github/workflows/qa-accessibility.yml; Playwright comes from the harness's
# package-lock.json. Bumping CI bumps the container.
#
# The base image is pinned by digest, as the production Dockerfile's is, so the
# same commit builds against the same bytes.
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04@sha256:d94c97dd9cacf183d0a6fd12a8e87b526e9e928307674ae9c94139139c0c6eae

COPY .github/workflows/ci.yml .github/workflows/qa-accessibility.yml .devcontainer/install-toolchain.sh /tmp/toolchain/
RUN bash /tmp/toolchain/install-toolchain.sh /tmp/toolchain /usr/local && rm -rf /tmp/toolchain

ENV PATH=/usr/local/go/bin:/home/vscode/go/bin:${PATH} \
    GOTOOLCHAIN=local \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

# Browsers and the system libraries each engine links against, installed as
# root at build time so the harness needs no sudo afterwards. The version is
# the one package-lock.json pins; postCreateCommand's `npm ci` installs the
# same package into the workspace, so the two cannot disagree.
COPY docs/qa/browser/package.json docs/qa/browser/package-lock.json /tmp/qa/
RUN cd /tmp/qa \
    && npm ci --ignore-scripts \
    && npx playwright install --with-deps chromium firefox webkit \
    && chmod -R a+rX /ms-playwright \
    && rm -rf /tmp/qa /root/.npm
