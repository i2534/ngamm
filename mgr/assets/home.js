function init(hasToken, ngaPostBase) {
    const origin = window.location.origin;
    const headers = {};
    const sorts = { key: 'Id', order: 'desc' };
    const pageSize = 15;
    let lastList = new Date();
    let topics = [];
    let currentPage = 1;

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
            showTokenSection();
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
            throw new Error('Failed to fetch topics');
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

    function renderTopics() {
        const start = (currentPage - 1) * pageSize;
        const paginated = topics.slice(start, start + pageSize);

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
            <td>${topic.Title}</td>
            <td><span class="author">${topic.Author}</span></td>
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

        renderPagination();
    }

    function renderSubscribe(container) {
        const authors = new Map();
        const changeStatus = (author, subscribed) => {
            const nss = authors.get(author);
            if (nss) {
                nss.forEach(ns => {
                    ns.title = subscribed ? '点击取消订阅' : '点击订阅';
                    ns.classList.remove(subscribed ? 'unsubscribed' : 'subscribed');
                    ns.classList.add(subscribed ? 'subscribed' : 'unsubscribed');
                });
            }
        };
        container.querySelectorAll('span.author').forEach(span => {
            const author = span.innerText;
            const ns = document.createElement('span');
            ns.innerHTML = `&#9733;`;
            ns.classList.add('subscribe');
            ns.addEventListener('click', () => {
                const subscribed = ns.classList.contains('subscribed');
                if (subscribed) {
                    if (!confirm(`确认要取消订阅 ${author} ?`)) {
                        return;
                    }
                }
                fetch(`${origin}/subscribe/${encodeURIComponent(author)}`, {
                    headers,
                    method: subscribed ? 'DELETE' : 'POST'
                })
                    .then(async (r) => {
                        const data = await r.json();
                        if (r.ok) {
                            changeStatus(author, !subscribed);
                        } else {
                            alert('操作失败', data.error);
                        }
                    });
            });
            span.insertAdjacentElement('afterend', ns);
            let nss = authors.get(author);
            if (!nss) {
                nss = new Set();
                authors.set(author, nss);
            }
            nss.add(ns);
        });

        fetch(`${origin}/subscribe/batch?${Date.now()}`, {
            headers: {
                ...headers,
                'Content-Type': 'application/json'
            },
            method: 'POST',
            body: JSON.stringify([...authors.keys()])
        }).then(async (r) => {
            const data = await r.json();
            if (r.ok) {
                Object.entries(data).forEach(([author, subscribed]) => {
                    changeStatus(author, subscribed);
                });
            }
        });
    }

    function renderPagination() {
        const totalPages = Math.ceil(topics.length / pageSize);
        const pagination = document.getElementById('pagination');
        pagination.innerHTML = '';

        const firstButton = document.createElement('button');
        firstButton.innerText = '|<';
        firstButton.onclick = () => {
            currentPage = 1;
            renderTopics();
        }
        pagination.appendChild(firstButton);

        const prevButton = document.createElement('button');
        prevButton.innerText = '<';
        prevButton.onclick = () => {
            currentPage = Math.max(1, currentPage - 1);
            renderTopics();
        }
        pagination.appendChild(prevButton);

        const nextButton = document.createElement('button');
        nextButton.innerText = '>';
        nextButton.onclick = () => {
            currentPage = Math.min(totalPages, currentPage + 1);
            renderTopics();
        }
        pagination.appendChild(nextButton);

        const lastButton = document.createElement('button');
        lastButton.innerText = '>|';
        lastButton.onclick = () => {
            currentPage = totalPages;
            renderTopics();
        }
        pagination.appendChild(lastButton);

        for (let i = 1; i <= totalPages; i++) {
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
            alert(error.message);
        }
    }

    async function createTopic() {
        let id = document.getElementById('createId').value.trim();
        if (!id) {
            alert('请输入帖子 ID 或链接');
            return;
        }
        if (!/^\d+$/.test(id)) {
            const match = id.match(/tid=(\d+)/);
            if (match) {
                id = match[1];
            } else {
                alert('帖子 ID 格式错误');
                return;
            }
        }
        try {
            const response = await fetch(`${origin}/topic/${id}`, { method: 'PUT', headers });
            const data = await response.json();
            if (!response.ok) {
                throw new Error(data.error);
            }
            alert(`帖子 ${data} 创建成功`);
            // listTopics();
            const topic = await fetchTopics(id);
            if (topic) {
                topics.push(topic);
                renderTopics();
            }
        } catch (error) {
            alert(error.message);
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
                alert(`删除帖子 ${data} 成功`);
                // listTopics();
                dealTopic(id, (_, index) => {
                    topics.splice(index, 1);
                    renderTopics();
                });
            } catch (error) {
                alert(error.message);
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
        closeDialog();
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
            alert(`更新计划 ${data} 成功`);
            // listTopics();

            const topic = await fetchTopics(id);
            if (topic) {
                dealTopic(id, (_, index) => {
                    topics[index] = topic;
                    renderTopics();
                });
            }
        } catch (error) {
            alert(error.message);
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
        window.open(`${origin}/view/${token}/${id}?max=${maxFloor}`, '_blank');
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
            alert(`帖子 ${data} 已加入更新队列`);
            // listTopics();
            const topic = await fetchTopics(id);
            if (topic) {
                dealTopic(id, (_, index) => {
                    topics[index] = topic;
                    renderTopics();
                });
            }
        } catch (error) {
            alert(error.message);
        }
    }

    window.setAuthToken = setAuthToken;
    window.listTopics = listTopics;
    window.sortTopics = sortTopics;
    window.createTopic = createTopic;
    window.deleteTopic = deleteTopic;
    window.schedTopic = schedTopic;
    window.viewTopic = viewTopic;
    window.freshTopic = freshTopic;
    window.submitSched = submitSched;
    window.closeDialog = closeDialog;
    window.clearInput = (id) => document.getElementById(id).value = '';

    window.addEventListener('load', () => {
        loadAuthToken();
        listTopics();
        setInterval(listTopics, 60000);
    });
}

