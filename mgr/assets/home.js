function init(hasToken, ngaBase) {
    const origin = window.location.origin;
    const ngaPostBase = `${ngaBase}/read.php?tid=`;
    const headers = {};
    const sorts = { key: 'Id', order: 'desc' };
    const pageSize = 15;
    let lastList = new Date();
    let topics = [];
    let currentPage = 1;
    let searchText = ''; // 添加搜索文本变量

    function dealTopic(id, callback) {
        if (!id || !topics || topics.length === 0) {
            return;
        }
        const index = topics.findIndex(t => t.Id === id || t.Id === parseInt(id));
        if (index >= 0) {
            const topic = topics[index];
            if (topic) {
                callback(topic, index);
            }
        }
    }

    function setAuthToken() {
        const token = document.getElementById('authToken').value;
        if (token && token.trim() !== '') {
            headers.Authorization = `${token}`;
            localStorage.setItem('token', token);
        }
        listTopics();
    }

    function loadAuthToken() {
        if (!hasToken) {
            return;
        }
        const token = localStorage.getItem('token');
        if (token) {
            document.getElementById('authToken').value = token;
            headers.Authorization = token;
            // showTokenSection();
        }
    }

    function showTokenSection() {
        document.getElementById('tokenSection').classList.remove('hidden');
    }

    async function fetchTopics(id) {
        const hs = { ...headers };
        const inc = !id && topics && topics.length > 0;
        if (inc) {
            hs['If-Modified-Since'] = lastList.toUTCString();
        }
        const response = await fetch(`${origin}/topic${id ? ('/' + id) : ''}`
            , { headers: hs }
        );
        if (response.status === 401) {
            showTokenSection();
            throw new Error('需要设置 Token');
        }
        if (!response.ok) {
            throw new Error('获取帖子列表失败');
        }
        const ret = await response.json();
        if (!id) {
            lastList = new Date();
        }
        if (!inc) {
            return ret;
        }

        const cache = new Map(ret.map(t => [t.Id, t]));
        const ts = [...topics];
        ts.forEach(t => {
            const ct = cache.get(t.Id);
            if (ct) {
                Object.assign(t, ct);
                cache.delete(t.Id);
            }
        });
        ts.push(...cache.values());
        return ts;
    }

    // 添加搜索函数
    function searchTopics(text) {
        searchText = text.toLowerCase();
        currentPage = 1; // 重置为第一页
        renderTopics();
    }

    function renderTopics() {
        // 过滤列表
        const filteredTopics = searchText ? topics.filter(topic =>
            String(topic.Id).includes(searchText) ||
            topic.Title.toLowerCase().includes(searchText) ||
            topic.Author.toLowerCase().includes(searchText)
        ) : topics;

        const start = (currentPage - 1) * pageSize;
        const paginated = filteredTopics.slice(start, start + pageSize);

        const ths = `
        <tr>
            <th key="Id" onclick="sortTopics('Id')">ID</th>
            <th key="Title" onclick="sortTopics('Title')">标题</th>
            <th key="Author" onclick="sortTopics('Author')">楼主</th>
            <th key="MaxFloor" onclick="sortTopics('MaxFloor')">楼层数</th>
            <th key="Result.Time" onclick="sortTopics('Result.Time')">最后更新于</th>
            <th>更新计划</th>
            <th>操作</th>
        </tr>`;

        const rows = paginated.map(topic => `
        <tr>
            <td><a href="${ngaPostBase}${topic.Id}" target="_blank">${topic.Id}</a></td>
            <td><span class="title" title="${topic.Title}">${topic.Title}</span></td>
            <td><span class="author" uid="${topic.Uid}"><a href="${ngaBase}/nuke.php?func=ucp&uid=${topic.Uid}}" target="_blank">${topic.Author}<a></span></td>
            <td>${topic.MaxFloor}</td>
            <td><span class="update-${topic.Result.Success ? 'success' : 'failed'}">${topic.Result.Time}</span></td>
            <td>${topic.Metadata.UpdateCron}</td>
            <td>
                <button onclick="viewTopic(${topic.Id}, ${topic.MaxFloor})" title="查看帖子内容">查看</button>
                <button class="fresh-button" onclick="freshTopic(${topic.Id})" title="立即更新帖子">更新</button>
                <button class="sched-button" onclick="schedTopic(${topic.Id})" title="任务计划更新帖子">计划</button>
                <button class="delete-button" onclick="deleteTopic(${topic.Id})" title="删除帖子到回收站">删除</button>
            </td>
        </tr>`).join('');

        const table = `<table>${ths}${rows}</table>`;
        const container = document.getElementById('topics');
        container.innerHTML = table;

        const th = container.querySelector(`th[key="${sorts.key}"]`);
        if (th) {
            th.classList.add(sorts.order === 'asc' ? 'sorted-asc' : 'sorted-desc');
        }

        renderSubscribe(container);

        renderPagination(filteredTopics);
    }

    const userSpans = new Map(), userInfos = new Map();;
    const changeSubscribeStatus = (uid, subscribed) => {
        const spans = userSpans.get(uid);
        if (spans) {
            spans.forEach(span => {
                const filter = ((userInfos.get(uid) || {}).filter || []).join('\n\t');
                span.title = subscribed ? `点击取消订阅${(filter && filter.length) ? '\n当前过滤规则:\n  ' + filter : ''}` : '点击订阅';
                span.classList.remove(subscribed ? 'unsubscribed' : 'subscribed');
                span.classList.add(subscribed ? 'subscribed' : 'unsubscribed');
            });
        }
    };
    function renderSubscribe(container) {
        userSpans.clear();
        container.querySelectorAll('span.author').forEach(span => {
            const author = span.innerText;
            const uid = parseInt(span.getAttribute('uid'));
            const ns = document.createElement('span');
            ns.innerHTML = `&#9733;`;
            ns.classList.add('subscribe');
            ns.addEventListener('click', () => {
                const subscribed = ns.classList.contains('subscribed');
                if (subscribed) {
                    if (!confirm(`确认要取消订阅 ${author} ?`)) {
                        return;
                    }
                    fetch(`${origin}/subscribe/${uid}`, {
                        headers,
                        method: 'DELETE'
                    })
                        .then(async (r) => {
                            const data = await r.json();
                            if (r.ok) {
                                changeSubscribeStatus(uid, !subscribed);
                            } else {
                                showAlert('操作失败', data.error);
                            }
                        });
                } else {
                    const dialog = document.getElementById('subscribeDialog');
                    if (dialog) {
                        document.getElementById('uid').value = uid;
                        const user = userInfos.get(uid);
                        document.getElementById('subFilter').value = (user && user.filter) ? user.filter.join('\n') : '';
                        dialog.showModal();
                    }
                }
            });
            span.insertAdjacentElement('afterend', ns);
            let nss = userSpans.get(uid);
            if (!nss) {
                nss = new Set();
                userSpans.set(uid, nss);
            }
            nss.add(ns);
        });

        fetch(`${origin}/subscribe/batch?${Date.now()}`, {
            headers: {
                ...headers,
                'Content-Type': 'application/json'
            },
            method: 'POST',
            body: JSON.stringify([...userSpans.keys()])
        }).then(async (r) => {
            const data = await r.json();
            if (r.ok) {
                Object.entries(data).forEach(([uv, info]) => {
                    const uid = parseInt(uv);
                    userInfos.set(uid, info);
                    changeSubscribeStatus(uid, info.subscribed);
                });
            }
        });
    }

    function renderPagination(topics) {
        const totalPages = Math.ceil(topics.length / pageSize);
        const pagination = document.getElementById('pagination');
        pagination.innerHTML = '';

        const totalTopics = document.createElement('span');
        totalTopics.innerText = `共 ${topics.length} 条 `;
        pagination.appendChild(totalTopics);

        const firstButton = document.createElement('button');
        firstButton.innerText = '⏮';
        firstButton.onclick = () => {
            currentPage = 1;
            renderTopics();
        }
        pagination.appendChild(firstButton);

        const prevButton = document.createElement('button');
        prevButton.innerText = '◀';
        prevButton.onclick = () => {
            currentPage = Math.max(1, currentPage - 1);
            renderTopics();
        }
        pagination.appendChild(prevButton);

        const nextButton = document.createElement('button');
        nextButton.innerText = '▶';
        nextButton.onclick = () => {
            currentPage = Math.min(totalPages, currentPage + 1);
            renderTopics();
        }
        pagination.appendChild(nextButton);

        const lastButton = document.createElement('button');
        lastButton.innerText = '⏭';
        lastButton.onclick = () => {
            currentPage = totalPages;
            renderTopics();
        }
        pagination.appendChild(lastButton);

        // 需要显示的页码集合
        const pagesToShow = new Set();
        // 始终显示前2页
        for (let i = 1; i <= Math.min(2, totalPages); i++) {
            pagesToShow.add(i);
        }
        // 始终显示最后2页
        for (let i = Math.max(1, totalPages - 1); i <= totalPages; i++) {
            pagesToShow.add(i);
        }
        // 显示当前页
        pagesToShow.add(currentPage);
        // 显示当前页前后2页
        for (let i = currentPage - 2; i <= currentPage + 2; i++) {
            if (i > 0 && i <= totalPages) {
                pagesToShow.add(i);
            }
        }
        // 将页码转为数组并排序
        const pageArray = Array.from(pagesToShow).sort((a, b) => a - b);
        // 渲染页码按钮
        let prevPage = 0;
        for (const page of pageArray) {
            // 在页码之间添加省略号
            if (page - prevPage > 1) {
                addEllipsis();
            }

            addPageButton(page);
            prevPage = page;
        }

        // 辅助函数：添加页码按钮
        function addPageButton(i) {
            const pb = document.createElement('button');
            pb.innerText = i;
            pb.onclick = () => {
                currentPage = i;
                renderTopics();
            };
            if (i === currentPage) {
                pb.classList.add('active');
            }
            pagination.appendChild(pb);
        }

        // 辅助函数：添加省略号
        function addEllipsis() {
            const ellipsis = document.createElement('span');
            ellipsis.innerText = '...';
            ellipsis.classList.add('pagination-ellipsis');
            pagination.appendChild(ellipsis);
        }
    }

    function sortTopics(key) {
        if (key && key.length) {
            if (sorts.key === key) {
                sorts.order = sorts.order === 'asc' ? 'desc' : 'asc';
            } else {
                sorts.key = key;
                sorts.order = 'asc';
            }
        } else {
            key = sorts.key;
        }

        topics.sort((a, b) => {
            let av = a[key], bv = b[key];
            if (av < bv) return sorts.order === 'asc' ? -1 : 1;
            if (av > bv) return sorts.order === 'asc' ? 1 : -1;
            return 0;
        });

        renderTopics();
    }

    async function listTopics() {
        try {
            topics = await fetchTopics();
            sortTopics();
        } catch (error) {
            showAlert(error.message);
        }
    }

    async function createTopic() {
        let id = document.getElementById('createId').value.trim();
        if (!id) {
            showAlert('请输入帖子 ID 或链接');
            return;
        }
        if (!/^\d+$/.test(id)) {
            const match = id.match(/tid=(\d+)/);
            if (match) {
                id = match[1];
            } else {
                showAlert('帖子 ID 格式错误');
                return;
            }
        }
        try {
            const response = await fetch(`${origin}/topic/${id}`, { method: 'PUT', headers });
            const data = await response.json();
            if (!response.ok) {
                throw new Error(data.error);
            }
            showAlert(`帖子 ${data} 创建成功`);
            // listTopics();
            const topic = await fetchTopics(id);
            if (topic) {
                topics.push(topic);
                renderTopics();
            }
        } catch (error) {
            showAlert(error.message);
        }
    }

    async function deleteTopic(id) {
        if (confirm(`确认要删除帖子 ${id} ?`)) {
            try {
                const response = await fetch(`${origin}/topic/${id}`, { method: 'DELETE', headers });
                const data = await response.json();
                if (!response.ok) {
                    throw new Error(data.error);
                }
                showAlert(`删除帖子 ${data} 成功`);
                // listTopics();
                dealTopic(id, (_, index) => {
                    topics.splice(index, 1);
                    renderTopics();
                });
            } catch (error) {
                showAlert(error.message);
            }
        }
    }

    async function schedTopic(id) {
        const dialog = document.getElementById('schedDialog');
        if (dialog) {
            document.getElementById('TopicID').value = id;
            const md = topics.find(t => t.Id === id).Metadata;
            for (const name of ['UpdateCron', 'MaxRetryCount']) {
                document.getElementById(name).value = md[name];
            }
            dialog.showModal();
        }
    }
    async function submitSched() {
        closeDialog('schedDialog');
        const id = document.getElementById('TopicID').value;
        const cron = document.getElementById('UpdateCron').value.trim();
        const maxRetryCount = document.getElementById('MaxRetryCount').value;
        try {
            const response = await fetch(`${origin}/topic/${id}`, {
                method: 'POST',
                headers: { ...headers, 'Content-Type': 'application/json' },
                body: JSON.stringify({ UpdateCron: cron, MaxRetryCount: parseInt(maxRetryCount) })
            });
            const data = await response.json();
            if (!response.ok) {
                throw new Error(data.error);
            }
            showAlert(`更新计划 ${data} 成功`);
            // listTopics();

            const topic = await fetchTopics(id);
            if (topic) {
                dealTopic(id, (_, index) => {
                    topics[index] = topic;
                    renderTopics();
                });
            }
        } catch (error) {
            showAlert(error.message);
        }
    }

    async function hashToken(token) {
        if (crypto && crypto.subtle) {// 这坑爹的API, 只在 https 下才能用
            return crypto.subtle.digest('SHA-1', new TextEncoder().encode(token))
                .then(hashBuffer => {
                    return Array.from(new Uint8Array(hashBuffer))
                        .map(b => b.toString(16).padStart(2, '0'))
                        .filter((_, i) => i % 5 == 0)
                        .join('');
                });
        } else {
            return new Promise((resolve) => {
                const calc = () => {
                    const hash = sha1(token);
                    const ret = [];
                    for (let i = 0; i < hash.length; i += 2) {
                        if (i % 10 === 0) {
                            ret.push(hash.substring(i, i + 2));
                        }
                    }
                    return ret.join('');
                }
                if (typeof sha1 === 'undefined') {
                    const script = document.createElement('script');
                    script.src = 'https://cdn.jsdelivr.net/npm/sha-1';
                    script.onload = () => {
                        resolve(calc());
                    };
                    document.head.appendChild(script);
                } else {
                    return resolve(calc());
                }
            });
        }
    }

    async function viewTopic(id, maxFloor) {
        const token = headers.Authorization ? await hashToken(headers.Authorization) : '-';
        window.open(`${origin}/view/${token}/${id}?max=${maxFloor}&vwm=${document.getElementById('viewWithoutMedia').checked}`, '_blank');
    }

    function closeDialog(dialogId) {
        const dialog = dialogId ? document.getElementById(dialogId) : document.querySelector('dialog[open]');
        if (dialog) {
            dialog.close();
        }
    }

    async function freshTopic(id) {
        try {
            const response = await fetch(`${origin}/topic/fresh/${id}`, { method: 'POST', headers });
            const data = await response.json();
            if (!response.ok) {
                throw new Error(data.error);
            }
            showAlert(`帖子 ${data} 已加入更新队列`);
            // listTopics();
            const topic = await fetchTopics(id);
            if (topic) {
                dealTopic(id, (_, index) => {
                    topics[index] = topic;
                    renderTopics();
                });
            }
        } catch (error) {
            showAlert(error.message);
        }
    }

    function showAlert(message) {
        closeDialog('alertDialog');
        const dialog = document.getElementById('alertDialog');
        document.getElementById('alertMessage').textContent = message;
        dialog.showModal();
    }

    window.setAuthToken = setAuthToken;
    window.listTopics = listTopics;
    window.sortTopics = sortTopics;
    window.createTopic = createTopic;
    window.deleteTopic = deleteTopic;
    window.schedTopic = schedTopic;
    window.viewTopic = viewTopic;
    window.freshTopic = freshTopic;
    window.showAlert = showAlert;
    window.submitSched = submitSched;
    window.closeDialog = closeDialog;
    window.clearInput = (id) => document.getElementById(id).value = '';
    window.submitSubscribe = async () => {
        closeDialog('subscribeDialog');
        const uv = document.getElementById('uid').value;
        const filter = document.getElementById('subFilter').value.split('\n').map(s => s.trim()).filter(s => s.length > 0);
        try {
            const response = await fetch(`${origin}/subscribe/${uv}`, {
                headers,
                method: 'POST',
                body: JSON.stringify(filter)
            });
            const data = await response.json();
            if (!response.ok) {
                throw new Error(data.error);
            }
            const uid = parseInt(uv);
            userInfos.set(uid, data);
            changeSubscribeStatus(uid, data.subscribed);
        } catch (error) {
            showAlert(error.message);
        }
    };


    window.viewWithoutMedia = function () {
        localStorage.setItem('withoutMedia', 'true');
    };

    window.addEventListener('load', () => {
        loadAuthToken();
        listTopics();

        document.getElementById('searchInput').addEventListener('input', (e) => searchTopics(e.target.value));
        document.getElementById('clearSearchInput').addEventListener('click', () => {
            clearInput('searchInput');
            searchTopics('');
        });

        const vwm = document.getElementById('viewWithoutMedia');
        vwm.addEventListener('change', (e) => {
            if (e.target.checked) {
                localStorage.setItem('withoutMedia', 'true');
            } else {
                localStorage.removeItem('withoutMedia');
            }
        });
        if (localStorage.getItem('withoutMedia') === 'true') {
            vwm.checked = true;
        }

        setInterval(listTopics, 60000);
    });
}

