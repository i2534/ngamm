function render(ngaBase, id, token, content, replaceAttachment) {
    const origin = window.location.origin;
    const baseUrl = `${origin}/view/${token}/${id}/`;
    const ngaPostBase = `${ngaBase}/read.php?tid=`;
    const ngaAttachBase = `https://img.nga.178.com/attachments/`;
    marked.use(markedBaseUrl.baseUrl(baseUrl));

    const urlParams = new URLSearchParams(window.location.search);
    const vwm = urlParams.get('vwm') == "true"; // view without media

    const attrSrc = '_src', attrPoster = '_poster';

    const renderer = {
        heading({ tokens, depth }) {
            const text = this.parser.parseInline(tokens);
            if (depth === 3) {// 标题
                return `<h${depth}><a href="${ngaPostBase}${id}" target="_blank">${text}</a></h${depth}>`;
            }
            if (depth === 5) {// 楼层
                let value = text.replace(/<span id="pid\d+">(.*?)<\/span>/g, '$1:'); // 与回复的统一化
                value = value.replace(/(\d+)\.\[\d+\]\s*<pid:(\d+)>\s*(\d{4}-\d{2}-\d{2}\s*\d{2}:\d{2}:\d{2})\s*by\s*(.+?)(\(\d+\))?:/g,
                    `<h${depth} floor="$1">
                        <div id="pid$2" class="floor">
                            <span class="num">$1</span><span class="author" uid="$5">$4</span><span class="time">$3</span>
                        </div>
                    </h${depth}>`);
                return value;
            }
            return `<h${depth}>${text}</h${depth}>`;
        },
        image({ href, text, title }) {
            return makeMedia(href, href, title || text);
        },
        link({ href, text, title }) {
            return makeLink(href, text, title);
        },
        text({ text }) {
            return text.replace(/\n/g, '<br>')
                .replace(/\[color(=(.+?))?\](.*?)\[\/color\]/gs, (_m, _, color, text) => {
                    return `<span style="color:${color || 'inherit'}">${text}</span>`;
                })
                .replace(/\[font(=(.+?))?\](.*?)\[\/font\]/gs, (_m, _, font, text) => {
                    return `<span style="font-family:${font || 'inherit'}">${text}</span>`;
                })
        },
    };
    function findNgaSmileName(src, title) {
        if (src.includes('.nga.') && src.includes('/smile/')) {
            let name = src.substring(src.lastIndexOf('/') + 1);
            if (name === '' && title) {// FIX NG娘表情 name 缺失
                name = 'ng_' + encodeURIComponent(title);
            }
            return name;
        }
        return '';
    }
    function fixSrc(src, title) {
        if (src && src.startsWith('./')) {
            return baseUrl + src.substring(2);
        }
        const name = findNgaSmileName(src, title);
        if (name !== '') {// 强制将链接到 NGA 的表情图片转换到本服务器
            return `${origin}/view/${token}/smile/${name}`;
        }
        return makeAttachSrc(src);
    }
    function makeMedia(src, name, title, poster) {
        const ext = name.split('.').pop().toLowerCase();
        if (['mp4', 'webm', 'ogg'].includes(ext)) {
            return makeVideo(src, title, poster);
        } else {
            return `<img ${attrSrc}="${fixSrc(src, title)}" alt="${title}" title="${title}" onerror="tryReloadImage(this)">`;
        }
    }
    function makeLink(href, text, title) {
        let target = '';
        if (!href.startsWith('#')) {
            target = ' target="_blank"';
        }
        const src = fixSrc(href, title);
        let ret = `<a href="${src}" title="${title || text}"${target}>${text === 'url' ? href : text}</a>`;

        if (src && src.includes('https://pan.')) {
            ret = `${ret}<span class="netpan" ${attrSrc}="${src}"></span>`;
        }
        return ret;
    }
    function makeVideo(src, title, poster) {
        let extra = '';
        if (title && title !== '') {
            extra += ` title="${title}"`;
        }
        extra += ` ${attrPoster}="${fixSrc(poster || '')}"`;
        return `<video ${attrSrc}="${fixSrc(src)}"${extra} controls onerror="tryReloadVideo(this)"></video>`;
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
            return makeVideo(src, '', poster || '');
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
            } else {// NGA 服务器拒绝跨域访问, 那就让服务器做代理
                const name = findNgaSmileName(src, oldTitle);
                if (name !== '') {
                    img.src = `${origin}/view/${token}/smile/${name}`;
                }
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
    function makeAttachSrc(src, floor) {
        if (!src.startsWith(ngaAttachBase)) {
            return src;
        }
        if (replaceAttachment !== true && replaceAttachment !== 'true') {
            return src;
        }

        if (floor === undefined || floor === null) {
            floor = '-1';
        }
        return `${baseUrl}at_${floor}_${escape(src)}`;
    }
    function tryReloadVideo(video) {
        video.onerror = null; // 防止进入无限循环

        const floor = findFloor(video);
        video.poster = makeAttachSrc(video.poster, floor);
        video.src = makeAttachSrc(video.src, floor);
    }
    // 修正引用, > 会被处理成 blockquote, 但 [quote] 需要自行处理
    function fixQuote(html) {
        return html.replaceAll('[quote]', '<blockquote _type="tag">')
            .replaceAll('[/quote]', '</blockquote>');
    }
    // 修正 [attach]
    function fixAttach(html) {
        return html.replace(/\[attach\](.*?)\[\/attach\]/g, (_, m1) => {
            let src = m1.trim();
            if (m1.startsWith('./')) {
                src = ngaAttachBase + src.substring(2);
            }
            const url = new URL(src);
            return makeMedia(src, url.pathname);
        });
    }
    // 修正下挂评论和它后面的楼层标题
    function fixComment(html) {
        return html.replace(/\*---下挂评论---\*\s*(.*?)\s*\*---下挂评论---\*\s*/gs, (_, m1) => {
            return `<div class="comment"><div class="subtitle">评论</div>${marked.parse('##### ' + m1)}</div>\n\n----\n\n##### `;
        });
    }
    // 修正代码块, 在 md 中被处理成 <div class="quote">...</div>
    function fixCode(html) {
        return html.replace(/<div class="quote">(.*?)<\/div>/gs, (_, m1) => {
            const ta = document.createElement('textarea');
            ta.innerHTML = m1.trim();
            const value = ta.value;
            return '\n```\n' + value + '\n```\n';
        });
    }
    // 处理因为包裹在 html 标签内导致的无法被 marked 处理的链接
    function fixLink(html) {
        return html
            .replace(/\!\[(.+?)\]\((.+?)\)/g, (_, text, src) => {
                return makeMedia(src, src, text);
            })
            .replace(/\[(.+?)\]\((.+?)\)/g, (_, text, src) => {
                return makeLink(src, text);
            });
    }
    // 修正表情
    function fixEmoji(html) {
        return html.replace(/&amp;#(\d+);/g, '&#$1;');
    }

    window.tryReloadImage = tryReloadImage;
    window.tryReloadVideo = tryReloadVideo;
    window.collapse = () => { }; //阻止残留的报错: <div class="foldBox no"><div class="collapse_btn"><a href="javascript:;" onclick="collapse(this);">+</a>点击展开 ...</div>

    window.jumpToFloor = function () {
        const floor = document.querySelector('#floorInput').value.trim();
        if (floor === '') {
            return;
        }
        const target = document.querySelector(`h5[floor="${floor}"]`);
        if (target) {
            target.scrollIntoView({ behavior: 'smooth' });
        } else {
            alert('未找到指定楼层');
        }
    };
    window.toggleOptionMenu = function () {
        const menu = document.querySelector('#optionMenu');
        menu.classList.toggle('hidden');
    };
    window.copyTopicId = function () {
        const menu = document.querySelector('#optionMenu');
        const tid = menu.querySelector('#topicId').textContent;
        navigator.clipboard.writeText(tid).then(() => {
            // 显示复制成功的提示
            const copyButton = menu.querySelector('.copy-button');
            copyButton.textContent = '已复制';

            // 2秒后恢复按钮文本
            setTimeout(() => {
                copyButton.textContent = '复制';
            }, 2000);
        }).catch(err => {
            console.error('复制失败:', err);
            alert('复制ID失败，请手动复制');
        });
    };
    window.forceReload = function () {
        if (confirm('是否强制重新下载?')) {
            fetch(`${baseUrl}`, {
                method: 'DELETE',
            }).then(r => {
                if (r.ok) {
                    alert('强制重新下载成功');
                    window.close();
                } else {
                    alert(`强制重新下载失败: ${r.statusText}`);
                }
            }).catch(e => {
                console.error('请求失败:', e);
                alert(`强制重新下载失败: ${e}`);
            });
        }
    };
    window.toggleViewMedia = function () {
        const e = document.querySelector('#toggleViewMedia');
        const vwm = e.getAttribute('vwm') === 'true';
        if (vwm) {
            e.setAttribute('vwm', 'false');
            e.textContent = '隐藏显示图片';
            document.querySelectorAll('img, video')
                .forEach(e => {
                    e.classList.remove('hide-media');
                });
        } else {
            e.setAttribute('vwm', 'true');
            e.textContent = '显示隐藏图片';
            document.querySelectorAll('img, video')
                .forEach(e => {
                    e.classList.add('hide-media');
                });
        }
    }

    const observer = new IntersectionObserver((entries, observer) => {
        entries.filter(e => e.isIntersecting)
            .forEach(entry => {
                const tar = entry.target;
                if (tar.closest('blockquote, .comment') !== null) {
                    // quote 和 comment 下的图片和视频要被手工加载, 但是表情要显示
                    if (tar.getAttribute('title') === 'img' && !tar.hasAttribute('show')) {
                        const btn = document.createElement('button');
                        btn.textContent = '显示图片';
                        btn.classList.add('show');
                        btn.onclick = function () {
                            tar.setAttribute('show', '');
                            btn.insertAdjacentElement('afterend', tar);
                            btn.remove();
                        };
                        tar.insertAdjacentElement('afterend', btn);
                        tar.remove();
                    }
                }

                window.clearTimeout(tar.loadId);
                tar.loadId = setTimeout(() => {
                    const rect = tar.getBoundingClientRect();
                    if (rect.width > 0 && rect.height > 0) {
                        [attrSrc, attrPoster].forEach(n => {
                            if (tar.hasAttribute(n)) {
                                tar.setAttribute(n.substring(1), fixSrc(tar.getAttribute(n)));
                                tar.removeAttribute(n);
                            }
                        });
                        observer.unobserve(tar);

                        switch (tar.tagName.toLowerCase()) {
                            case 'img':
                            case 'video':
                                tar.addEventListener('click', function () {
                                    displayBigMedia(tar);
                                });
                                break;
                        }
                    }
                }, 100);
            });
    });

    function displayBigMedia(media) {
        // 创建一个遮罩层
        const overlay = document.createElement('div');
        overlay.classList.add('overlay');
        // 点击遮罩层关闭
        overlay.addEventListener('click', () => {
            overlay.remove();
        });
        // 将克隆的媒体元素添加到遮罩层
        const nm = document.createElement(media.tagName);
        nm.src = media.src;
        overlay.appendChild(nm);
        // 将遮罩层添加到页面
        document.body.appendChild(overlay);
    }

    async function processNetPan(parent) {
        const pans = await fetch(`${origin}/pan/${token}/${id}?${Date.now()}`)
            .then(r => {
                if (r.ok) {
                    return r.json();
                } else {
                    console.log(`请求网盘数据失败: ${r.statusText}`);
                    return [];
                }
            });
        const sns = parent.querySelectorAll('span.netpan');
        for (const e of sns) {
            e.innerHTML = '';
        }

        if (Array.isArray(pans) && pans.length > 0) {
            sns.forEach(e => {
                const src = e.getAttribute(attrSrc);
                for (const pan of pans) {
                    if (pan.URL != src) {
                        continue;
                    }
                    if (e.children.length > 0) {
                        continue; // 兼容重复的记录
                    }
                    switch (pan.Status) {
                        case 'pending': {
                            const btn = document.createElement('button');
                            btn.classList.add('netpan-opt');
                            btn.classList.add('netpan-opt-save');
                            btn.innerHTML = '<i class="fa fa-floppy-o"></i>';
                            btn.title = '当前文件未保存, 点击保存到网盘';
                            btn.addEventListener('click', () => {
                                optNetPan('save', pan.URL, btn);
                            });
                            e.appendChild(btn);
                            break;
                        }
                        case 'failed': {
                            const btn = document.createElement('button');
                            btn.classList.add('netpan-opt');
                            btn.classList.add('netpan-opt-retry');
                            btn.innerHTML = '<i class="fa fa-refresh"></i>';
                            btn.title = `保存失败: ${pan.Message}, 点击重新保存到网盘`;
                            btn.addEventListener('click', () => {
                                optNetPan('retry', pan.URL, btn);
                            });
                            e.appendChild(btn);
                            // break; //也有可能是重复保存导致失败, 所以都可以有删除选项
                        }
                        case 'success': {
                            const btn = document.createElement('button');
                            btn.classList.add('netpan-opt');
                            btn.classList.add('netpan-opt-delete');
                            btn.innerHTML = '<i class="fa fa-trash"></i>';
                            btn.title = '当前文件已保存, 点击删除网盘文件';
                            btn.addEventListener('click', () => {
                                confirm('会删除当前帖子在网盘内的所有内容, 是否删除?') && optNetPan('delete', pan.URL, btn);
                            });
                            e.appendChild(btn);
                            break;
                        }
                    }

                }
            });
        }
    }
    function optNetPan(opt, url, tar) {
        tar.disabled = true;

        fetch(`${origin}/pan/${token}/${id}`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                opt,
                url
            })
        })
            .then(r => {
                if (r.ok) {
                    return r.json();
                } else {
                    console.log(`请求网盘数据失败: ${r.statusText}`);
                    alert('操作失败, 请稍后再试');
                    window.location.reload();
                    return null;
                }
            })
            .then(d => {
                if (d) {
                    const msg = d.error;
                    if (msg) {
                        alert(msg);
                        window.location.reload();
                    } else {
                        let i = 0;
                        const task = window.setInterval(() => {
                            if (i > 30) {
                                clearInterval(task);
                            } else {
                                processNetPan(document.querySelector('#content'));
                                i++;
                            }
                        }, 2000);
                    }
                }
            });
    }

    window.addEventListener('load', () => {
        if (vwm) {
            const btn = document.querySelector('#toggleViewMedia');
            btn.textContent = '显示隐藏图片';
            btn.setAttribute('vwm', 'true');
        }

        const c = document.querySelector('#content');
        if (c) {
            const loading = URL.createObjectURL(new Blob([document.querySelector('#loading').innerHTML], { type: 'image/svg+xml' }));

            let html = content;
            [fixQuote, fixAttach, fixComment, fixCode, fixEmoji].forEach(fix => {
                html = fix(html);
            })
            // 渲染
            html = marked.parse(html);
            html = fixLink(html);
            c.innerHTML = html;

            // 监视所有 img 和 video 元素的可见性
            c.querySelectorAll('img, video')
                .forEach(e => {
                    if (vwm) {
                        e.classList.add('hide-media');
                    }
                    if (e.tagName.toLowerCase() === 'img') {
                        e.src = loading;
                        e.addEventListener('load', function () { // 图片加载完毕后更新宽度
                            e.style.width = e.naturalWidth + 'px';
                        });
                    } else {
                        e.poster = loading;
                    }
                    observer.observe(e)
                });

            // 为所有 code 元素添加双击事件监听器
            c.querySelectorAll('code').forEach(e => {
                e.addEventListener('dblclick', () => {
                    const range = document.createRange();
                    range.selectNodeContents(e);
                    const selection = window.getSelection();
                    selection.removeAllRanges();
                    selection.addRange(range);
                });
            });

            // 限制跳转楼层的值
            const floors = Array.from(c.querySelectorAll('h5[floor]'))
                .map(e => parseInt(e.getAttribute('floor')));
            const floorInput = document.querySelector('#floorInput');
            floorInput.max = Math.max(...floors);
            floorInput.addEventListener('input', () => {
                const value = parseInt(floorInput.value);
                if (value < floorInput.min) {
                    floorInput.value = floorInput.min;
                } else if (value > floorInput.max) {
                    floorInput.value = floorInput.max;
                }
            });

            // 打开层主信息
            c.querySelectorAll('.floor>.author').forEach(e => {
                let uid = e.getAttribute('uid');
                let href;
                if (uid && uid !== '') {
                    if (uid.startsWith('(')) {
                        uid = uid.substring(1);
                    }
                    if (uid.endsWith(')')) {
                        uid = uid.substring(0, uid.length - 1);
                    }
                    href = `${ngaBase}/nuke.php?func=ucp&uid=${uid.trim()}`;
                } else {
                    href = `${ngaBase}/nuke.php?func=ucp&username=${GBK.URI.encodeURIComponent(e.textContent)}`;
                }

                const a = document.createElement('a');
                a.href = href;
                a.target = '_blank';
                a.textContent = e.textContent;
                a.className = e.className;
                if (uid && uid !== '') {
                    a.setAttribute('uid', uid);
                }

                e.replaceWith(a);
            });

            processNetPan(c);
        }
    });
}