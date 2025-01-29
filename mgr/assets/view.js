function render(ngaPostBase, id, token, content) {
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
        image({ href, text, title }) {
            return `<img ${attrSrc}="${href}" alt="${text}" title="${title || text}" onerror="tryReloadImage(this)">`;
        },
        link({ href, text, title }) {
            return makeLink(href, text, title);
        }
    };

    function makeLink(href, text, title) {
        let target = '';
        if (!href.startsWith('#')) {
            target = ' target="_blank"';
        }
        return `<a href="${href}" title="${title || text}"${target}>${text === 'url' ? href : text}</a>`;
    }

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
        const c = document.querySelector('#content');
        if (c) {
            let html = content;
            // 修正引用, > 会被处理成 blockquote, 但 [quote] 需要自行处理
            // html = html.replaceAll(/\[quote\](.*?)\[\/quote\]/gs, `<div class="quote">$1</div>`);
            html = html.replaceAll('[quote]', '<blockquote class="quote">').replaceAll('[/quote]', '</blockquote>');
            // 修正下挂评论和它后面的楼层标题
            html = html.replaceAll(/\*---下挂评论---\*\s*(.*?)\s*\*---下挂评论---\*\s*/gs, (_, m1) => {
                return `<div class="comment"><div class="subtitle">评论</div>${marked.parse('##### ' + m1)}</div>

----

##### `;
            });
            html = marked.parse(html);
            // 处理因为包裹在 html 标签内导致的无法被 marked 处理的链接
            html = html.replaceAll(/\[(.+?)\]\((.+?)\)/g, (_, text, src) => {
                return makeLink(src, text);
            });
            c.innerHTML = html;
            const observer = new IntersectionObserver((entries, observer) => {
                entries.filter(e => e.isIntersecting)
                    .forEach(entry => {
                        const tar = entry.target;
                        if (tar.closest('blockquote, .comment') !== null) {
                            // quote 和 comment 下的图片和视频要被手工加载, 但是表情要显示
                            if (tar.getAttribute('title') === 'img') {
                                const btn = document.createElement('button');
                                btn.textContent = '显示图片';
                                btn.classList.add('show');
                                btn.onclick = function () {
                                    btn.insertAdjacentElement('afterend', tar);
                                    btn.remove();
                                };
                                tar.insertAdjacentElement('afterend', btn);
                                tar.remove();
                            }
                        }
                        [attrSrc, attrPoster].forEach(n => {
                            if (tar.hasAttribute(n)) {
                                tar.setAttribute(n.substring(1), tar.getAttribute(n));
                                tar.removeAttribute(n);
                            }
                        });
                        observer.unobserve(tar);
                    });
            });
            c.querySelectorAll('img, video').forEach(e => observer.observe(e));
        }
    });
}