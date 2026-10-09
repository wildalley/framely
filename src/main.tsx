import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { SelectionOverlay } from "./SelectionOverlay";
import "./styles.css";

// Region-picker overlays load the same bundle with `?overlay`.
const isOverlay = new URLSearchParams(window.location.search).has("overlay");

createRoot(document.getElementById("root")!).render(
  <StrictMode>{isOverlay ? <SelectionOverlay /> : <App />}</StrictMode>,
);
