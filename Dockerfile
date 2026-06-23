# feelc as a Docker rule-engine service. The engine is CGO-free and network-free by default, so the
# image is a single static binary on distroless — no shell, no package manager, minimal surface.
#
#   docker build -t feelc .
#   docker run --rm -p 8080:8080 -v "$PWD/sample-project:/work" feelc      # serve a project + UI
#
# Mount a project directory (a set of .rules modules + feelc.project.json) at /work. Edits made in the
# UI are persisted back to the mounted volume; --watch hot-reloads external changes.

# ---- build ----
FROM golang:1.23-alpine AS build
WORKDIR /src
# Copy the whole module first: go.mod uses a local `replace` for the vendored feel fork
# (./third_party/feel), so module resolution needs the tree present.
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/feelc ./cmd/feelc

# ---- runtime ----
# :nonroot runs as uid 65532 (no root). The default CMD is READ-ONLY: the module-writing endpoints
# (PUT/POST/DELETE /v1/modules) stay disabled unless you add `--allow-edit` — and that should only be
# done on a trusted/loopback host, with /work writable by uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/feelc /usr/local/bin/feelc
USER nonroot:nonroot
# A project workspace is mounted here (override with your own volume).
VOLUME ["/work"]
EXPOSE 8080
ENTRYPOINT ["feelc"]
# Read-only by default. To enable in-browser editing on a trusted host, append --allow-edit:
#   docker run --rm -p 127.0.0.1:8080:8080 -v "$PWD/proj:/work" feelc serve --project /work --ui --watch --allow-edit
CMD ["serve", "--project", "/work", "--addr", ":8080", "--ui", "--watch"]
