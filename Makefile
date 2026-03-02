# 与 test.sh 对应：build -> docker-build, run -> docker-run, go -> run, log -> changelog
.DEFAULT_GOAL := help

.PHONY: docker-build docker-run run changelog fetch_ngapost2md help

help:
	@echo "用法: make [docker-build|docker-run|run|changelog|fetch_ngapost2md]"
	@echo "  docker-build     - Docker 构建 (USE_LOCAL_SRC=true, NET_PAN=true)"
	@echo "  docker-run       - Docker 运行并进入 shell"
	@echo "  run              - 本地 go run（TOKEN=abc, -m data/n2md1.10/ngapost2md）"
	@echo "  changelog        - 生成 CHANGELOG.md"
	@echo "  fetch_ngapost2md - 按 ngapost2md.version 下载 ngapost2md 到 ./np2md"

docker-build:
	docker build --build-arg USE_LOCAL_SRC="true" --build-arg NET_PAN="true" -t ngamm-pan:1.0 .

docker-run:
	docker run -it --rm --name ngamm-pan -v ./data:/app/data ngamm-pan:1.0 sh

run:
	export TOKEN="abc" && go run . -m data/np2md/ngapost2md

changelog:
	git log --pretty=format:"- %ad (%h): %s " --date=format-local:'%Y-%m-%d %H:%M' > CHANGELOG.md

fetch_ngapost2md:
	sh entrypoint.sh fetch ./data
