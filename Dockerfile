# 第一阶段：构建应用程序
FROM golang:1.24-alpine AS builder
# 安装 git
RUN apk add --no-cache git
# 设置工作目录
WORKDIR /app

# 添加构建参数，默认从GitHub克隆
ARG USE_LOCAL_SRC="false"
ARG GHPROXY=""

# 根据USE_LOCAL_SRC参数决定使用本地代码还是从GitHub克隆
COPY . /tmp/src/
RUN if [ "$USE_LOCAL_SRC" = "true" ]; then \
    echo "Using local source code" && \
    cp -r /tmp/src/* /app/ && \
    rm -rf /tmp/src; \
    else \
    echo "Cloning from GitHub" && \
    git clone ${GHPROXY}https://github.com/i2534/ngamm.git . ; \
    fi

# 下载依赖
# ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
# 构建应用程序
RUN go build -ldflags "-X main.buildTime=$(date +%Y-%m-%dT%H:%M:%S) -X main.gitHash=$(git describe --tags --always) -X main.logFlags=0" -v -o main .
RUN ./main -v

# 第二阶段：创建运行时镜像
FROM alpine:latest

# 如果 --build-arg NET_PAN="true" 则使用网盘相关
ARG NET_PAN=""
ENV NET_PAN=${NET_PAN}

# 安装 ngapost2md 所需的共享库
RUN apk add --no-cache libc6-compat unzip curl
# 设置时区为东八区
RUN apk add --no-cache tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone \
    && apk del tzdata
# 设置工作目录
WORKDIR /app
# 从构建阶段复制二进制文件和脚本文件
COPY --from=builder /app/main ./ngamm
COPY --from=builder /app/assets/pan-config.ini ./pan-config.ini
COPY entrypoint.sh .
# 设置环境变量
# 访问 Token
# ENV TOKEN=""
# 帖子单独存放路径, 可以是绝对地址或相对地址(相对于 ngapost2md)
# ENV TOPIC_ROOT=""
# 赋予脚本执行权限并执行脚本
RUN chmod +x entrypoint.sh && sh entrypoint.sh prepare
# 挂载文件夹
VOLUME /app/data
# 暴露应用程序运行的端口
EXPOSE 5842

# 运行应用程序
CMD ["sh", "entrypoint.sh", "start"]