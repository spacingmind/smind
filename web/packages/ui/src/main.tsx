import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

function App() {
  return (
    <div style={{ fontFamily: "system-ui", padding: "3rem", color: "#eee", background: "#111", minHeight: "100vh" }}>
      <h1>smind</h1>
      <p>Spacing Mind — Space for your agents, free for you.</p>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
