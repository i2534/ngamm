# ngamm
为 [ngapost2md](https://github.com/ludoux/ngapost2md) 提供的一个简单的管理工具

可以执行 [Cron](https://godoc.org/github.com/robfig/cron) 任务

## 使用方式
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

#### 使用 API 管理

##### 获取已保存的帖子列表
使用 Postman, ApiPost 等 Rest API 测试工具访问
```
GET http://[ip]:5842/topic
```
或者使用 curl 
```
curl -X GET http://[ip]:5842/topic
```

##### 添加一个保存帖子的任务
使用 Postman, ApiPost 等 Rest API 测试工具访问
```
PUT http://[ip]:5842/topic/{id}
```
或者使用 curl 
```
curl -X PUT http://[ip]:5842/topic/{id}
```
将 `{id}` 替换成实际的帖子ID

##### 设置帖子定时更新
使用 Postman, ApiPost 等 Rest API 测试工具访问
```
POST http://[ip]:5842/topic/{id}
Content-Type: application/json

{
    "UpdateCron": "@every 1h"
}
```
或者使用 curl 
```
curl -X POST "http://[ip]:5842/topic/{id}" \
     -H "Content-Type: application/json" \
     -d '{
           "UpdateCron": "@every 1h"
         }'
```
将 `{id}` 替换成实际的帖子ID, UpdateCron 的取值可以参考 [Cron](https://godoc.org/github.com/robfig/cron) 的说明, `@every 1h` 表示每一个小时执行一次更新

##### 查看帖子内容
浏览器访问
```
http://[ip]:5842/view/{id}
```
将 `{id}` 替换成实际保存后的帖子ID