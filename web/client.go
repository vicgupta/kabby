package web

// clientJS is the JavaScript client for the web UI.
const clientJS = `
class KabbyClient {
  constructor() {
    this.ws = null;
    this.sessionId = null;
    this.connect();
  }

  connect() {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = protocol + '//' + location.host + '/api/stream/';
    this.ws = new WebSocket(url);
    this.ws.onopen = () => {
      document.getElementById('status-dot').className = 'dot online';
      document.getElementById('status-text').textContent = 'Connected';
    };
    this.ws.onclose = () => {
      document.getElementById('status-dot').className = 'dot offline';
      document.getElementById('status-text').textContent = 'Disconnected';
      setTimeout(() => this.connect(), 3000);
    };
    this.ws.onmessage = (evt) => {
      const data = JSON.parse(evt.data);
      this.renderMessage(data);
    };
  }

  renderMessage(data) {
    const chat = document.getElementById('chat');
    const div = document.createElement('div');
    div.className = 'message ' + (data.role || 'assistant');
    div.textContent = data.content || '';
    chat.appendChild(div);
    chat.scrollTop = chat.scrollHeight;
  }

  send(text) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ type: 'message', content: text }));
  }
}

document.addEventListener('DOMContentLoaded', () => {
  const client = new KabbyClient();
  const input = document.getElementById('input');
  const send = document.getElementById('send');
  send.addEventListener('click', () => {
    client.send(input.value);
    input.value = '';
  });
  input.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
      client.send(input.value);
      input.value = '';
    }
  });
});
`
