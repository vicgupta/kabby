package web

// pageHTML is the single-page web UI served by the hub.
const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>kabby</title>
<link rel="stylesheet" href="/styles.css">
</head>
<body>
<div id="app">
  <div class="sidebar">
    <h2>kabby</h2>
    <div class="workspace-picker">
      <label>Workspace</label>
      <select id="workspace"></select>
    </div>
    <div class="sessions">
      <h3>Sessions</h3>
      <ul id="session-list"></ul>
      <button id="new-session">New Session</button>
    </div>
  </div>
  <div class="main">
    <div class="status-bar">
      <span id="status-dot" class="dot offline"></span>
      <span id="status-text">Disconnected</span>
    </div>
    <div class="chat" id="chat"></div>
    <div class="input-area">
      <input type="text" id="input" placeholder="Type a message...">
      <button id="send">Send</button>
    </div>
  </div>
</div>
<script src="/client.js"></script>
</body>
</html>`
