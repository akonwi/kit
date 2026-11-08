// Pele with the diagram kinds the browser draws, loaded on its own when a
// reply first shows a diagram (ADR 0046, The browser client).
import { enableZoom, PeleError, parse, register, render } from "pele/core";
import flowchart from "pele/diagrams/flowchart";
import sequence from "pele/diagrams/sequence";

register(flowchart, sequence);

export { enableZoom, PeleError, parse, render };
