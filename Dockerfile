# 第一阶段：构建应用程序
FROM golang:1.23-alpine AS builder
# 安装 git
RUN apk add --no-cache git
# 设置工作目录
WORKDIR /app
# 从 GitHub 克隆代码
ENV GHPROXY=""
RUN git clone ${GHPROXY}https://github.com/i2534/ngamm.git .
# 下载依赖
# ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
# 构建应用程序
RUN go build -ldflags "-X main.buildTime=$(date +%Y-%m-%dT%H:%M:%S) -X main.gitHash=$(git describe --tags --always)" -v -o main .
RUN ./main -v

# 第二阶段：创建运行时镜像
FROM alpine:latest
# 安装 ngapost2md 所需的共享库
RUN apk add --no-cache libc6-compat
# 设置工作目录
WORKDIR /app
# 从构建阶段复制二进制文件和脚本文件
COPY --from=builder /app/main .
# COPY --from=builder /app/entrypoint.sh .
COPY entrypoint.sh .
# 赋予脚本执行权限并执行脚本
RUN chmod +x entrypoint.sh && sh entrypoint.sh fetch

# 设置环境变量
ENV TOKEN=""
# 挂载文件夹
VOLUME /app/data
# 暴露应用程序运行的端口
EXPOSE 5842

# 运行应用程序
CMD ["sh", "entrypoint.sh", "start"]