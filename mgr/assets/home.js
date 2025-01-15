function init(hasToken, ngaPostBase) {
    const origin = window.location.origin;
    const headers = {};
    const sorts = { key: 'Id', order: 'asc' };
    let topics = [];

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
        const response = await fetch(`${origin}/topic${id ? ('/' + id) : ''}`, { headers });
        if (response.status === 401) {
            showTokenSection();
            throw new Error('需要设置 Token');
        }
        if (!response.ok) {
            throw new Error('Failed to fetch topics');
        }
        return response.json();
    }

    function renderTopics() {
        const headers = `
        <tr>
            <th onclick="sortTopics('Id')">ID</th>
            <th onclick="sortTopics('Title')">标题</th>
            <th onclick="sortTopics('Author')">楼主</th>
            <th onclick="sortTopics('Result.Time')">最后更新于</th>
            <th>更新计划</th>
            <th>操作</th>
        </tr>`;

        const rows = topics.map(topic => `
        <tr>
            <td><a href="${ngaPostBase}${topic.Id}" target="_blank">${topic.Id}</a></td>
            <td>${topic.Title}</td>
            <td>${topic.Author}</td>
            <td><span class="update-${topic.Result.Success ? 'success' : 'failed'}">${topic.Result.Time}</span></td>
            <td>${topic.Metadata.UpdateCron}</td>
            <td>
                <button onclick="viewTopic(${topic.Id})" title="查看帖子内容">查看</button>
                <button class="fresh-button" onclick="freshTopic(${topic.Id})" title="立即更新帖子">更新</button>
                <button class="sched-button" onclick="schedTopic(${topic.Id})" title="任务计划更新帖子">计划</button>
                <button class="delete-button" onclick="deleteTopic(${topic.Id})" title="删除帖子到回收站">删除</button>
            </td>
        </tr>`).join('');

        const table = `<table>${headers}${rows}</table>`;
        document.getElementById('topics').innerHTML = table;
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
        let id = document.getElementById('createId').value;
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
                    delete topics[index];
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
        const cron = document.getElementById('UpdateCron').value;
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

    function hashToken(token) {
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

    async function viewTopic(id) {
        const token = headers.Authorization ? await hashToken(headers.Authorization) : '-';
        window.open(`${origin}/view/${token}/${id}`, '_blank');
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

    function setupSSE() {
        const es = new EventSource(`${origin}/sse`);
        es.onmessage = async function (event) {
            if (!event.data) {
                return;
            }
            console.log('SSE message:', event.data);
            const data = JSON.parse(event.data);
            if (data.event === 'topicUpdated') {
                const id = data.data;
                console.log('Topic updated:', id);
                const topic = await fetchTopics(id);
                if (topic) {
                    dealTopic(id, (_, index) => {
                        topics[index] = topic;
                        renderTopics();
                    });
                }
            }
        };
        es.onerror = function (event) {
            console.error('SSE error:', event);
        };
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

    window.addEventListener('load', () => {
        loadAuthToken();
        listTopics();
        setupSSE();
    });
}

