# ngamm
为 [ngapost2md](https://github.com/ludoux/ngapost2md) 提供的一个简单的管理工具

可以执行 [Cron](https://godoc.org/github.com/robfig/cron) 任务

## 部署方式
### docker 方式(推荐)

自动构建 NGAMM 并且整合 ngapost2md

你只需要提供正确的 [`config.ini`](https://github.com/ludoux/ngapost2md) 放到映射的 `data` 目录下

第一次启动容器后, 会在 `data` 目录下自动释放 `ngapost2md` 并生成 `config.ini`

必须配置项:
* `base_url`: 你访问 NGA 的地址, 如我这边就是 https://ngabbs.com
* `ua`: 你的浏览器的 `UserAgent`, 可以按 `F12`, 在控制台里输入 `navigator.userAgent` 就会输出
* `ngaPassportUid`: `F12` -> 应用程序 -> Cookie -> https://ngabbs.com
找到同名的字段, 点击, 在下面的 Cookie Value 中全选, 复制粘贴
* `ngaPassportCid`: 步骤同上

#### docker

```shell
docker pull i2534/ngamm:latest
docker run -it -p 5842:5842 -v ./data:/app/data -e TOKEN="" i2534/ngamm:latest
```

#### docker compose
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
      # 内网使用可以留空, 开放到外网, 最好设置一个复杂点的, 否则容易被攻击
      - TOKEN=
    restart: unless-stopped
```

### 单独程序方式(不推荐)
#### 准备 ngapost2md

先去 [ngapost2md](https://github.com/ludoux/ngapost2md) 下载最新的版本, 然后根据 [配置说明](https://github.com/ludoux/ngapost2md) 配置好, 确保单独使用 
```
./ngapost2md {id}
```
将 `{id}` 替换成实际的帖子ID, 可以正确下载到帖子

将配置好的 `ngapost2md` 程序 和 其同目录下的所有内容都放置到 `ngap2m` 文件夹里

#### 下载 ngamm

进入 [Actions](https://github.com/i2534/ngamm/actions/workflows/build.yml)
进入最后已成成功构建的 workflow , 在 `Artifacts` 中找到需要的程序下载解压

#### 配置 ngamm

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

## 使用

### 使用页面管理

浏览器访问 `url:port`

`port` 默认为 `5842`

## UI 和 功能
Home 页面:
![Home 截图](./assets/ui_home.png)
View 页面:
![View 截图](./assets/ui_view.png)