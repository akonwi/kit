import "./styles/theme.css";
import "./styles/app.css";
import { render } from "solid-js/web";
import { App } from "./app/App";

const root = document.getElementById("app");
if (root) render(() => <App />, root);
