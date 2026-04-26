FROM acr-openxlab-prod-registry-vpc.cn-shanghai.cr.aliyuncs.com/public/golang:1.25.6 as builder
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR $GOPATH/src
COPY . .
RUN go env -w GOPROXY=https://mirrors.aliyun.com/goproxy/,direct && go build -o /app/ack-vpa-updater

FROM acr-openxlab-prod-registry-vpc.cn-shanghai.cr.aliyuncs.com/public/alpine:latest
WORKDIR /app
COPY --from=builder /app .

ENV TZ=Asia/Shanghai

ENTRYPOINT ["./ack-vpa-updater"]
