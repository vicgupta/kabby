package web

// stylesCSS is the CSS for the web UI.
const stylesCSS = `
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0f172a; color: #e2e8f0; height: 100vh; overflow: hidden; }
#app { display: flex; height: 100vh; }
.sidebar { width: 300px; background: #1e293b; border-right: 1px solid #334155; padding: 16px; display: flex; flex-direction: column; gap: 16px; overflow-y: auto; }
.sidebar h2 { font-size: 18px; font-weight: 600; color: #f8fafc; }
.main { flex: 1; display: flex; flex-direction: column; background: #0f172a; }
.status-bar { padding: 8px 16px; background: #1e293b; border-bottom: 1px solid #334155; display: flex; align-items: center; gap: 8px; font-size: 12px; }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #ef4444; }
.dot.online { background: #22c55e; }
.chat { flex: 1; overflow-y: auto; padding: 24px; display: flex; flex-direction: column; gap: 16px; }
.input-area { padding: 16px; border-top: 1px solid #334155; display: flex; gap: 8px; background: #1e293b; }
.input-area input { flex: 1; background: #0f172a; border: 1px solid #334155; color: #e2e8f0; padding: 8px 12px; border-radius: 6px; }
.input-area button { background: #3b82f6; color: white; border: none; padding: 8px 16px; border-radius: 6px; cursor: pointer; }
.input-area button:hover { background: #2563eb; }
.message { max-width: 70%; padding: 10px 14px; border-radius: 10px; line-height: 1.4; }
.message.user { background: #3b82f6; align-self: flex-end; }
.message.assistant { background: #1e293b; border: 1px solid #334155; align-self: flex-start; }
.message.system { font-size: 11px; color: #94a3b8; padding: 4px 8px; }
.sessions ul { list-style: none; padding: 0; margin: 0; }
.sessions li { padding: 8px; border-radius: 6px; cursor: pointer; }
.sessions li:hover { background: #334155; }
button { background: #3b82f6; color: white; border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; font-size: 14px; }
button:hover { background: #2563eb; }
select, input { width: 100%; padding: 6px; background: #0f172a; border: 1px solid #334155; color: #e2e8f0; border-radius: 4px; }
`
