function render(ngaPostBase, id, token) {
    const origin = window.location.origin;
    const baseUrl = `${origin}/view/${token}/${id}/`;
    marked.use(markedBaseUrl.baseUrl(baseUrl));

    const attrSrc = '_src', attrPoster = '_poster';

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
            return `<img ${attrSrc}="${href}" alt="${text}" title="${text}" onerror="tryReloadImage(this)">`;
        }
    };

    const extensions = [{
        name: 'video',
        level: 'inline',
        start(src) {
            return src.indexOf('<video');
        },
        tokenizer(src) {
            const match = src.match(/^.*<video[^>]*src="([^"]+)"[^>]*poster="([^"]+)"[^>]*>.*<\/video>.*$/);
            if (match) {
                return {
                    type: 'video',
                    raw: match[0],
                    src: match[1],
                    poster: match[2],
                };
            }
            return false;
        },
        renderer({ src, poster }) {
            return `<video ${attrSrc}="${src}" ${attrPoster}="${poster}" controls onerror="tryReloadVideo(this)"></video>`;
        }
    }];
    marked.use({ renderer, extensions });

    function tryReloadImage(img) {
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
    function escape(src) {// 转义, 不用 %2F 是因为使用代理服务器时会被提前解码为 / 导致 404
        return encodeURIComponent(src).replaceAll('%2F', '_2F');
    }
    function tryReloadVideo(video) {
        video.onerror = null; // 防止进入无限循环

        const floor = findFloor(video);
        video.poster = `${baseUrl}at_${floor}_${escape(video.poster)}`;
        video.src = `${baseUrl}at_${floor}_${escape(video.src)}`;
    }

    window.tryReloadImage = tryReloadImage;
    window.tryReloadVideo = tryReloadVideo;
    window.addEventListener('load', () => {
        const content = document.querySelector('#content');
        content.innerHTML = marked.parse(content.innerHTML);

        const observer = new IntersectionObserver((entries, observer) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const tar = entry.target;
                    [attrSrc, attrPoster].forEach(n => {
                        if (tar.hasAttribute(n)) {
                            tar.setAttribute(n.substring(1), tar.getAttribute(n));
                            tar.removeAttribute(n);
                        }
                    });
                    observer.unobserve(tar);
                }
            });
        });
        content.querySelectorAll('img, video').forEach(e => observer.observe(e));
        content.classList.remove('hidden');
    });
}