# P3 完善：多阶段构建 + distroless + 非 root
# 注意：golang:1.27-alpine tag 需在 P3 用真实 Docker 验证（见 architecture.md A5）。
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/userapi ./cmd/userapi

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/userapi /userapi
EXPOSE 8080
ENTRYPOINT ["/userapi"]
