import { HttpAgent } from '@tencent/ag-ui-client-js';

const DEFAULT_ENDPOINT = 'http://localhost:8080/agui';

const promptInput = document.getElementById('prompt');
const submitButton = document.getElementById('submit');
const statusEl = document.getElementById('status');
const logEl = document.getElementById('log');

if (!promptInput || !submitButton || !statusEl || !logEl) {
  throw new Error('Required elements are missing.');
}

let isRunning = false;

submitButton.addEventListener('click', async () => {
  if (isRunning) {
    return;
  }
  const prompt = promptInput.value.trim();
  if (!prompt) {
    setStatus('请输入问题后再提交。');
    return;
  }

  isRunning = true;
  submitButton.disabled = true;
  clearLog();
  appendMessage('user', prompt);
  setStatus('正在处理中...');

  const agent = createAgent();
  const ui = createUiHandlers(logEl);
  const subscription = agent.subscribe(ui.handlers);

  try {
    await agent.addMessage({ role: 'user', content: prompt });
    await agent.runAgent();
    setStatus('已完成。');
  } catch (error) {
    console.error(error);
    setStatus('请求失败，请查看控制台。');
    ui.appendError(`Run failed: ${error instanceof Error ? error.message : String(error)}`);
  } finally {
    subscription.unsubscribe?.();
    ui.reset();
    submitButton.disabled = false;
    isRunning = false;
  }
});

function createAgent() {
  return new HttpAgent({
    url: DEFAULT_ENDPOINT,
    debug: false
  });
}

function createUiHandlers(log) {
  let assistantBody = null;
  let toolBody = null;

  return {
    handlers: {
      onRunStartedEvent: ({ event }) => {
        appendMessage('info', `Run started: ${event.runId}`, log);
      },
      onTextMessageStartEvent: () => {
        assistantBody = ensureMessageBody('assistant', '');
      },
      onTextMessageContentEvent: ({ event }) => {
        assistantBody = assistantBody ?? ensureMessageBody('assistant', '');
        assistantBody.textContent += event.delta ?? '';
        scrollToBottom();
      },
      onTextMessageEndEvent: () => {
        assistantBody = null;
      },
      onToolCallStartEvent: ({ event }) => {
        toolBody = ensureMessageBody('tool', `Call Tool ${event.toolCallName}\n`);
      },
      onToolCallArgsEvent: ({ event }) => {
        toolBody = toolBody ?? ensureMessageBody('tool', '');
        toolBody.textContent += event.delta ?? '';
        scrollToBottom();
      },
      onToolCallResultEvent: ({ event }) => {
        appendMessage('tool', `Tool result: ${event.content}`, log);
        toolBody = null;
      },
      onCustomEvent: ({ event }) => {
        const payload = event.payload ?? event.data ?? event.value ?? null;
        const payloadText =
          payload === null
            ? 'Empty payload'
            : typeof payload === 'string'
            ? payload
            : JSON.stringify(payload, null, 2);
        appendMessage('custom', payloadText, log, event.name);
      },
      onRunFinishedEvent: ({ result }) => {
        appendMessage('info', result !== undefined ? `Run finished, result: ${result}` : 'Run finished', log);
      },
      onRunFailedEvent: ({ error }) => {
        appendMessage('error', `Run failed: ${error}`, log);
      }
    },
    appendError: (text) => {
      appendMessage('error', text, log);
    },
    reset: () => {
      assistantBody = null;
      toolBody = null;
    }
  };

  function ensureMessageBody(role, content) {
    const message = appendMessage(role, content, log);
    return message.querySelector('.message-body');
  }

  function scrollToBottom() {
    log.scrollTop = log.scrollHeight;
  }
}

function appendMessage(role, text, target = logEl, eventName) {
  const wrapper = document.createElement('div');
  wrapper.className = `message ${role}`;

  const label = document.createElement('strong');
  label.textContent = roleLabel(role, eventName);

  const body = document.createElement('div');
  body.className = 'message-body';
  body.textContent = text;

  wrapper.appendChild(label);
  wrapper.appendChild(body);
  target.appendChild(wrapper);
  target.scrollTop = target.scrollHeight;
  return wrapper;
}

function roleLabel(role, eventName) {
  if (role === 'assistant') return 'Assistant';
  if (role === 'user') return 'User';
  if (role === 'tool') return 'Tool';
  if (role === 'error') return 'Error';
  if (role === 'custom') return eventName ? `Custom: ${eventName}` : 'Custom';
  return 'Info';
}

function clearLog() {
  logEl.innerHTML = '';
}

function setStatus(text) {
  statusEl.textContent = text;
}
