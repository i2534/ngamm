# ngamm
为 [ngapost2md](https://github.com/ludoux/ngapost2md) 提供的一个简单的管理工具

可以执行 [Cron](https://godoc.org/github.com/robfig/cron) 任务

## docker 方式(推荐)

自动构建 NGAMM 并且整合 ngapost2md

你只需要提供正确的 [`config.ini`](https://github.com/ludoux/ngapost2md) 放到映射的 `data` 目录下

### docker

```sh
docker pull i2534/ngamm:latest
docker run -it -p 5842:5842 -v ./data:/app/data -e TOKEN="" i2534/ngamm:latest
```

### docker compose
```yaml
services:
  ngamm:
    image: i2534/ngamm:latest
    container_name: ngamm
    ports:
      - "5842:5842"
    volumes:
      - ./data:/app/data
    environment:
      - TOKEN=
    restart: unless-stopped
```

## 单独程序方式
### 准备 ngapost2md

先去 [ngapost2md](https://github.com/ludoux/ngapost2md) 下载最新的版本, 然后根据 [配置说明](https://github.com/ludoux/ngapost2md) 配置好, 确保单独使用 
```
./ngapost2md {id}
```
将 `{id}` 替换成实际的帖子ID, 可以正确下载到帖子

将配置好的 `ngapost2md` 程序 和 其同目录下的所有内容都放置到 `ngap2m` 文件夹里

### 下载 ngamm

进入 [Actions](https://github.com/i2534/ngamm/actions/workflows/build.yml)
进入最后已成成功构建的 workflow , 在 `Artifacts` 中找到需要的程序下载解压

### 配置 ngamm

去到 https://github.com/i2534/ngamm/actions/runs/12195299390 , 找到 `Artifacts` 里下载 ngamm 程序(目前只有 linux 版本)
将程序解压放到 `ngap2m` 文件夹同目录下, 执行
```
chmod +x ngamm
```
给与程序可执行权限

```
./ngamm -p 5842 -m ngap2m/ngapost2md
```
启动 `ngamm`, 看到 `Server started, listening on :5842` 表示启动成功

### 正式使用

#### 使用页面管理

浏览器访问 `url:port`

`port` 默认为 `5842`