#!/bin/bash

# 主函数
main() {
    local cmd=$1

    case "$cmd" in
        build)
            docker build --build-arg USE_LOCAL_SRC="true" --build-arg NET_PAN="true" -t ngamm-pan:1.0 .
            ;;
        run)
            docker run -it --rm --name ngamm-pan  -v ./data:/app/data ngamm-pan:1.0 sh
            ;;
        go)
            # go run . -m data/ngapost2md -n data/pan
            go run . -m data/ngapost2md -l simple
            ;;
        log)
            git log --pretty=format:"- %ad (%h): %s " --date=format-local:'%Y-%m-%d %H:%M' > CHANGELOG.md
            ;;
        *)
            echo "未知命令: $cmd"
            exit 1
            ;;
    esac
}

main "$1"