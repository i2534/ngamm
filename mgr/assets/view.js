function render(ngaPostBase, id, token) {
    const origin = window.location.origin;
    const baseUrl = `${origin}/view/${token}/${id}/`;
    marked.use(markedBaseUrl.baseUrl(baseUrl));

    function tryReload(img) {
        img.onerror = null; // 防止进入无限循环

        const oldTitle = img.title;
        const oldCursor = img.style.cursor;

        let isReloading = false;
        const reload = function () {
            if (isReloading) {
                return;
            }
            isReloading = true;

            let src = img.src;
            const i = src.indexOf('?t=');
            if (i !== -1) {
                src = src.substring(0, i);
            }
            if (src.startsWith(origin)) {
                img.src = src + '?t=' + new Date().getTime(); // 添加时间戳以强制重新加载
            } else if (src.indexOf('.nga.') != -1 && src.indexOf('/smile/') != -1) {// NGA 服务器拒绝跨域访问, 那就让服务器做代理
                const name = src.substring(src.lastIndexOf('/') + 1);
                img.src = `${origin}/view/${token}/smile/${name}`;
            }
            img.style.cursor = oldCursor;
            img.title = oldTitle;

            setTimeout(() => {
                isReloading = false;
            }, 1000);
        };

        img.onerror = function () {
            img.onerror = null; // 防止进入无限循环
            img.style.cursor = 'pointer';
            img.title = '点击重载';
            img.addEventListener('click', reload);
        }

        img.addEventListener('load', function () {
            img.removeEventListener('click', reload);
        });

        reload();
    }
    const renderer = {
        heading({ tokens, depth }) {
            const text = this.parser.parseInline(tokens);
            if (depth === 3) {
                return `<h${depth}><a href="${ngaPostBase}${id}" target="_blank">${text}</a></h${depth}>`;
            }
            if (depth === 5) {// 楼层
                let floor = text.match(/(\d+)\.\[\d+\]/);
                if (floor) {
                    floor = floor[1];
                } else {
                    floor = '';
                }
                return `<h${depth} floor=${floor}>${text.replaceAll(/&lt;.+?&gt;/g, '')}</h${depth}>`;
            }
            return `<h${depth}>${text}</h${depth}>`;
        },
        image({ href, text }) {
            return `<img src="${href}" alt="${text}" title="${text}" loading="lazy" onerror="tryReload(this)">`;
        }
    };
    marked.use({ renderer });

    const content = document.querySelector('#content');
    content.innerHTML = marked.parse(content.innerHTML);

    const vs = content.querySelectorAll('video');
    if (vs && vs.length > 0) {
        function findFloor(e) {
            while (e) {
                let prev = e.previousElementSibling;
                while (prev) {
                    if (prev.tagName.toLowerCase() === 'h5') {
                        return prev.getAttribute('floor');
                    }
                    prev = prev.previousElementSibling;
                }
                e = e.parentElement;
            }
            return null;
        }
        vs.forEach(v => {
            v.onplay = null;
            v.onplaying = null;
            v.controls = true;
            v.style.cursor = 'pointer';
            v.title = '点击播放';
            v.addEventListener('click', function () {
                if (v.paused) {
                    v.play();
                } else {
                    v.pause();
                }
            });
            v.onerror = function () {
                v.onerror = null;
                const floor = findFloor(v);
                v.src = `${baseUrl}at_${floor}_${encodeURIComponent(v.src)}`;
            };
        });
    }
}