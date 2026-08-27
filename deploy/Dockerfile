# SPDX-FileCopyrightText: 2026 Ryan Madhuwala
# SPDX-License-Identifier: MIT

# Stage 1: build the TypeScript UI.
FROM node:22-alpine AS ui
WORKDIR /ui
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: build the static Go binary with the UI embedded.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /out/linkedin-api ./cmd/server

# Stage 3: minimal runtime with only CA certificates and the binary.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/linkedin-api /linkedin-api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/linkedin-api"]
