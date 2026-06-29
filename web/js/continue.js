// continue.js - Continue button + SSE consumption + interrupt
(function () {
    'use strict';
    var isRunning = false;
    var currentAssistant = null;
    var abortController = null;
    var streamingTitle = null;
    var messagesBeforeStream = 0;
    var toastTimer = null;
    var reconnectMode = false;
    var stopSoundTimer = null;
    var stopSoundContext = null;
    var activeAskToolCallId = null;

    function showToast(msg) {
        var toast = document.getElementById('error-toast');
        if (!toast) {
            toast = document.createElement('div');
            toast.id = 'error-toast';
            document.body.appendChild(toast);
        }
        if (toastTimer) {
            clearTimeout(toastTimer);
        }
        toast.textContent = msg;
        toast.classList.add('visible');
        toastTimer = setTimeout(function () {
            toast.classList.remove('visible');
        }, 2500);
    }

    function scrollIfNearBottom(container) {
        var threshold = 80;
        if (container.scrollHeight - container.scrollTop - container.clientHeight < threshold) {
            container.scrollTop = container.scrollHeight;
        }
    }

    var btnContinue = document.getElementById('btn-continue');

    function setRunning(running) {
        isRunning = running;
        var noobActive = window.NoobMode && window.NoobMode.isActive();
        if (running) {
            btnContinue.textContent = 'Interrupt';
            btnContinue.classList.add('interrupt');
            btnContinue.disabled = noobActive;
            if (ChatList.getCurrentTitle()) {
                localStorage.setItem('streaming_chat', ChatList.getCurrentTitle());
            }
        } else {
            btnContinue.textContent = 'Send';
            btnContinue.classList.remove('interrupt');
            btnContinue.disabled = false;
            localStorage.removeItem('streaming_chat');
        }
    }

    btnContinue.addEventListener('click', function () {
        if (isRunning && window.NoobMode && window.NoobMode.isActive()) return;
        if (isRunning) {
            doInterrupt();
        } else {
            doContinue();
        }
    });

    document.getElementById('user-input').addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            if (isRunning && window.NoobMode && window.NoobMode.isActive()) return;
            if (isRunning) {
                doInterrupt();
            } else {
                doContinue();
            }
        }
    });

    async function doInterrupt() {
        try {
            await fetch('/api/chat/interrupt', {method: 'POST'});
        } catch (e) {
        }
        if (abortController) {
            abortController.abort();
            abortController = null;
        }
        currentAssistant = null;
        streamingTitle = null;
        setRunning(false);
    }

    async function doContinue(reconnect) {
        stopStopSound();
        var title = ChatList.getCurrentTitle();
        if (!title) {
            showToast('Please create or open a session');
            return;
        }

        var inputArea = document.getElementById('user-input');
        var input = inputArea.value.trim();
        var autoContinue = document.getElementById('auto-continue').checked;

        if (!reconnect) {
            setRunning(true);
        }
        reconnectMode = !!reconnect;
        abortController = new AbortController();
        inputArea.value = '';
        currentAssistant = null;
        streamingTitle = title;

        // Remember how many messages are already rendered
        var container = document.getElementById('messages');
        // Remove any leftover streaming assistant (from auto-resume reconnect)
        var streamingEl = container.querySelector('.assistant-streaming');
        if (streamingEl) {
            streamingEl.remove();
        }
        messagesBeforeStream = container.querySelectorAll('.message').length;

        try {
            var resp = await fetch('/api/chat/continue', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    title: title,
                    input: input || '',
                    auto_continue: autoContinue,
                    reconnect: !!reconnect
                }),
                signal: abortController.signal
            });

            if (!resp.ok) {
                var err = await resp.json();
                showToast(err.error || 'Unknown');
                setRunning(false);
                return;
            }

            if (reconnect) {
                setRunning(true);
            }

            var reader = resp.body.getReader();
            var decoder = new TextDecoder();
            var buffer = '';

            try {
                while (true) {
                    var _a = await reader.read(), done = _a.done, value = _a.value;
                    if (done) break;

                    buffer += decoder.decode(value, {stream: true});
                    var lines = buffer.split('\n');
                    buffer = lines.pop() || '';

                    var eventType = '';
                    var eventData = '';

                    for (var i = 0; i < lines.length; i++) {
                        var line = lines[i];
                        if (line.startsWith('event: ')) {
                            eventType = line.substring(7).trim();
                        } else if (line.startsWith('data: ')) {
                            eventData = line.substring(6).trim();
                        } else if (line === '') {
                            if (eventType && eventData) {
                                handleEvent(eventType, eventData);
                            }
                            eventType = '';
                            eventData = '';
                        }
                    }
                }
            } catch (e) {
                if (e.name === 'AbortError') {
                    return;
                }
                showToast('Stream error: ' + (e.message || e));
            }
        } catch (e) {
            if (e.name === 'AbortError') {
                return;
            }
            showToast('Continue error: ' + (e.message || e));
        } finally {
            reconnectMode = false;
            setRunning(false);
            abortController = null;
            playStopSound();
            await ChatList.loadMessages();
        }
    }

    function playStopSound() {
        if (localStorage.getItem('stop_sound') !== 'true') return;
        stopStopSound();
        playBeep();
        if (localStorage.getItem('loop_stop_sound') === 'true') {
            stopSoundTimer = setInterval(playBeep, 1200);
            setTimeout(function () {
                document.addEventListener('pointerdown', stopStopSound, {once: true});
                document.addEventListener('keydown', stopStopSound, {once: true});
            }, 0);
        }
    }

    function stopStopSound() {
        if (stopSoundTimer) {
            clearInterval(stopSoundTimer);
            stopSoundTimer = null;
        }
    }

    function playBeep() {
        try {
            var AudioCtx = window.AudioContext || window.webkitAudioContext;
            if (!AudioCtx) return;
            if (!stopSoundContext) stopSoundContext = new AudioCtx();
            var ctx = stopSoundContext;
            if (ctx.state === 'suspended') ctx.resume();

            var osc = ctx.createOscillator();
            var gain = ctx.createGain();
            osc.type = 'sine';
            osc.frequency.setValueAtTime(880, ctx.currentTime);
            osc.frequency.setValueAtTime(660, ctx.currentTime + 0.12);
            gain.gain.setValueAtTime(0.001, ctx.currentTime);
            gain.gain.exponentialRampToValueAtTime(0.18, ctx.currentTime + 0.02);
            gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.3);
            osc.connect(gain);
            gain.connect(ctx.destination);
            osc.start();
            osc.stop(ctx.currentTime + 0.32);
        } catch (e) {
        }
    }

    function handleEvent(type, data) {
        // Guard: ignore events from a different chat (user switched chats mid-stream)
        if (streamingTitle && ChatList.getCurrentTitle() !== streamingTitle) {
            return;
        }

        var evt;
        try {
            evt = JSON.parse(data);
        } catch (e) {
            return;
        }

        switch (type) {
            case 'delta':
                appendToAssistant(evt.content || evt.Content, 'content');
                break;
            case 'reasoning_delta':
                appendToAssistant(evt.content || evt.Content, 'reasoning');
                break;
            case 'tool_call':
                if (!(window.NoobMode && window.NoobMode.isActive())) {
                    appendToolCall(evt.tool_call || {id: evt.ID, function: {name: evt.Name, arguments: evt.Args}});
                }
                break;
            case 'tool_result':
                if (!(window.NoobMode && window.NoobMode.isActive())) {
                    appendToolResult(evt.tool_result ? evt.tool_result.message : evt);
                }
                break;
            case 'assistant_done':
                break;
            case 'tool_execute':
                break;
            case 'user_added':
                appendUserMessage(evt.content || evt.Content || '');
                break;
            case 'error':
                if (evt.error && evt.error.ids) {
                    evt.error.ids.forEach(function (id) {
                        var el = document.querySelector('.tool-call-item[data-tool-call-id="' + id + '"]');
                        if (el) {
                            el.classList.add('highlight-error');
                            setTimeout(function () {
                                el.classList.remove('highlight-error');
                            }, 5000);
                        }
                    });
                }
                if (evt.error && !reconnectMode) {
                    showToast(evt.error.detail || evt.error.type || 'Unknown error');
                }
                break;
        }
    }

    function getOrCreateAssistant() {
        var container = document.getElementById('messages');

        if (currentAssistant && currentAssistant.parentNode === container) {
            return currentAssistant;
        }

        // Always create a fresh assistant - never reuse stale DOM
        if (currentAssistant) {
            currentAssistant.remove();
        }
        currentAssistant = document.createElement('div');
        currentAssistant.className = 'message role-assistant assistant-streaming';
        container.appendChild(currentAssistant);
        scrollIfNearBottom(container);
        return currentAssistant;
    }

    function appendToAssistant(text, field) {
        var el = getOrCreateAssistant();
        if (field === 'reasoning') {
            var reasoningEl = el.querySelector('.reasoning-block');
            if (!reasoningEl) {
                reasoningEl = document.createElement('div');
                reasoningEl.className = 'reasoning-block';
                var toggle = document.createElement('div');
                toggle.className = 'reasoning-toggle';
                toggle.textContent = 'Reasoning \u25B6';
                var contentEl = document.createElement('div');
                contentEl.className = 'reasoning-content';
                contentEl.style.display = 'none';
                contentEl.dataset.rawText = '';
                toggle.addEventListener('click', function () {
                    if (contentEl.style.display === 'none') {
                        contentEl.style.display = 'block';
                        toggle.textContent = 'Reasoning \u25BC';
                    } else {
                        contentEl.style.display = 'none';
                        toggle.textContent = 'Reasoning \u25B6';
                    }
                });
                reasoningEl.appendChild(toggle);
                reasoningEl.appendChild(contentEl);
                el.appendChild(reasoningEl);
            }
            var contentEl = reasoningEl.querySelector('.reasoning-content');
            contentEl.dataset.rawText = (contentEl.dataset.rawText || '') + text;
            contentEl.innerHTML = marked.parse(contentEl.dataset.rawText);
        } else {
            var contentEl = el.querySelector('.msg-content');
            if (!contentEl) {
                contentEl = document.createElement('div');
                contentEl.className = 'msg-content';
                contentEl.dataset.rawText = '';
                el.appendChild(contentEl);
            }
            contentEl.dataset.rawText = (contentEl.dataset.rawText || '') + text;
            contentEl.innerHTML = marked.parse(contentEl.dataset.rawText);
        }
        scrollIfNearBottom(document.getElementById('messages'));
    }

    function appendToolCall(tc) {
        var name = tc.function ? tc.function.name : (tc.Name || tc.name || '');
        var args = tc.function ? tc.function.arguments : (tc.Args || tc.arguments || '{}');
        var id = tc.id || tc.ID || '';

        var el = getOrCreateAssistant();
        var tcBlock = el.querySelector('.tool-calls-block');
        if (!tcBlock) {
            tcBlock = document.createElement('div');
            tcBlock.className = 'tool-calls-block';
            var tcToggle = document.createElement('div');
            tcToggle.className = 'tool-calls-toggle';
            tcToggle.textContent = 'Tool Calls (1) \u25B6';
            var tcl = document.createElement('div');
            tcl.className = 'tool-calls-list';
            tcl.style.display = 'none';
            tcToggle.addEventListener('click', function () {
                var count = tcl.querySelectorAll('.tool-call-item').length;
                if (tcl.style.display === 'none') {
                    tcl.style.display = 'flex';
                    tcToggle.textContent = 'Tool Calls (' + count + ') \u25BC';
                } else {
                    tcl.style.display = 'none';
                    tcToggle.textContent = 'Tool Calls (' + count + ') \u25B6';
                }
            });
            tcBlock.appendChild(tcToggle);
            tcBlock.appendChild(tcl);
            el.appendChild(tcBlock);
        }

        var tcl = tcBlock.querySelector('.tool-calls-list');
        var tcToggle = tcBlock.querySelector('.tool-calls-toggle');

        var existing = tcl.querySelector('.tool-call-item[data-tool-call-id="' + id + '"]');
        var item;
        if (existing) {
            item = existing;
        } else {
            item = document.createElement('div');
            item.className = 'tool-call-item';
            item.dataset.toolCallId = id;
            tcl.appendChild(item);
        }

        try {
            var formatted = JSON.stringify(JSON.parse(args), null, 2);
        } catch (e) {
            formatted = args;
        }
        item.innerHTML = '<div class="tool-call-name">' + Messages.escHtml(name) + '</div>'
            + '<div class="tool-call-args">' + Messages.escHtml(formatted) + '</div>';

        var count = tcl.querySelectorAll('.tool-call-item').length;
        if (tcl.style.display === 'none') {
            tcToggle.textContent = 'Tool Calls (' + count + ') \u25B6';
        } else {
            tcToggle.textContent = 'Tool Calls (' + count + ') \u25BC';
        }

        scrollIfNearBottom(document.getElementById('messages'));
    }

    function appendToolResult(msg) {
        var container = document.getElementById('messages');
        var div = document.createElement('div');
        div.className = 'message role-tool';

        var headerDiv = document.createElement('div');
        headerDiv.className = 'msg-header';
        headerDiv.innerHTML = '<span class="msg-role">TOOL</span><span class="msg-tags"><span class="msg-tag">' + Messages.escHtml(msg.name || '') + '</span></span>';

        var contentDiv = document.createElement('div');
        contentDiv.className = 'msg-content';
        contentDiv.style.display = 'none';
        contentDiv.innerHTML = '<pre><code>' + Messages.escHtml(msg.content || '') + '</code></pre>';

        var toolToggle = document.createElement('span');
        toolToggle.className = 'tool-result-toggle';
        toolToggle.textContent = ' \u25B6';
        toolToggle.addEventListener('click', function (e) {
            e.stopPropagation();
            if (contentDiv.style.display === 'none') {
                contentDiv.style.display = 'block';
                toolToggle.textContent = ' \u25BC';
            } else {
                contentDiv.style.display = 'none';
                toolToggle.textContent = ' \u25B6';
            }
        });
        headerDiv.appendChild(toolToggle);

        div.appendChild(headerDiv);
        div.appendChild(contentDiv);
        container.appendChild(div);

        currentAssistant = null;
        scrollIfNearBottom(document.getElementById('messages'));
    }

    function appendUserMessage(content) {
        var container = document.getElementById('messages');

        // Skip if this user message is already rendered (reconnect after refresh)
        var existing = container.querySelectorAll('.message.role-user');
        var msgs = container.querySelectorAll('.message');
        var currentCount = msgs.length;
        var renderedCount = currentCount - container.querySelectorAll('.assistant-streaming').length;
        // If this is a reconnect (no user input), the user message from the first event
        // may already be rendered. Check by looking at messages added after stream start.
        // But we cleared the streaming assistant, so messagesBeforeStream doesn't include it.
        // If the number of existing user messages >= messagesBeforeStream + "already rendered",
        // skip creating a duplicate. Simpler: always create, let loadMessages at the end fix it.

        var emptyState = container.querySelector('.empty-state');
        if (emptyState) {
            emptyState.remove();
        }

        var div = document.createElement('div');
        div.className = 'message role-user';
        div.innerHTML = '<div class="msg-header"><span class="msg-role">USER</span></div>'
            + '<div class="msg-content">' + marked.parse(content) + '</div>';
        container.appendChild(div);
        scrollIfNearBottom(container);
    }

    function tryAutoResume() {
        if (window.NoobMode && window.NoobMode.isActive()) return;
        var streamingTitle = localStorage.getItem('streaming_chat');
        if (streamingTitle && streamingTitle === ChatList.getCurrentTitle()) {
            doContinue(true);
        }
    }

    window.ContinueModule = {
        tryAutoResume: tryAutoResume,
        isRunning: function () {
            return isRunning;
        },
        doInterrupt: doInterrupt
    };

    function extractAskQuestion(tc) {
        var args = tc && tc.function ? tc.function.arguments : '{}';
        try {
            var parsed = JSON.parse(args || '{}');
            return parsed.question || parsed.prompt || 'The assistant needs more information.';
        } catch (e) {
            return 'The assistant needs more information.';
        }
    }

    function getPendingAsk(chat) {
        if (!chat || !chat.messages || chat.messages.length === 0) return null;
        var idx = chat.messages.length - 1;
        var last = chat.messages[idx];
        if (!last || last.role !== 'assistant' || !last.tool_calls || last.tool_calls.length === 0) return null;
        var tc = last.tool_calls[last.tool_calls.length - 1];
        if (!tc || !tc.function || tc.function.name !== 'ask_user' || !tc.id) return null;
        return {messageIndex: idx, toolCall: tc, question: extractAskQuestion(tc)};
    }

    function maybeShowAskUser(chat) {
        if (isRunning) return;
        var pending = getPendingAsk(chat);
        if (!pending) {
            closeAskUserModal();
            return;
        }
        if (activeAskToolCallId === pending.toolCall.id) return;
        showAskUserModal(pending);
    }

    function showAskUserModal(pending) {
        var overlay = document.getElementById('ask-user-overlay');
        var questionEl = document.getElementById('ask-user-question');
        var inputEl = document.getElementById('ask-user-input');
        var submitBtn = document.getElementById('ask-user-submit');
        if (!overlay || !questionEl || !inputEl || !submitBtn) return;

        activeAskToolCallId = pending.toolCall.id;
        questionEl.textContent = pending.question;
        inputEl.value = '';
        overlay.classList.remove('hidden');
        setTimeout(function () {
            inputEl.focus();
        }, 0);

        submitBtn.onclick = async function () {
            var answer = inputEl.value.trim();
            if (!answer) return;
            submitBtn.disabled = true;
            submitBtn.textContent = 'Submitting...';
            try {
                var title = ChatList.getCurrentTitle();
                var resp = await fetch('/api/chat/' + encodeURIComponent(title) + '/message/' + (pending.messageIndex + 1), {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({
                        role: 'tool',
                        name: 'ask_user',
                        tool_call_id: pending.toolCall.id,
                        content: answer,
                        send_to_server: true
                    })
                });
                if (resp.ok) {
                    closeAskUserModal();
                    await ChatList.loadMessages();
                } else {
                    var data = await resp.json();
                    showToast(data.error || 'Failed to submit answer');
                }
            } finally {
                submitBtn.disabled = false;
                submitBtn.textContent = 'Submit';
            }
        };
    }

    function closeAskUserModal() {
        var overlay = document.getElementById('ask-user-overlay');
        if (overlay) overlay.classList.add('hidden');
        activeAskToolCallId = null;
        stopStopSound();
    }

    document.getElementById('ask-user-close').addEventListener('click', closeAskUserModal);
    document.getElementById('ask-user-cancel').addEventListener('click', closeAskUserModal);
    document.getElementById('ask-user-overlay').addEventListener('click', function (e) {
        if (e.target === this) closeAskUserModal();
    });
    document.getElementById('ask-user-input').addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            document.getElementById('ask-user-submit').click();
        }
    });

    window.AskUserPrompt = {
        maybeShow: maybeShowAskUser
    };
})();
