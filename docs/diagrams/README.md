# Maintain the architecture diagrams

Each `.mmd` file is the authoritative source for its view. Its matching `.svg`
is a derived preview. Edit Mermaid, render SVG, and review both in the same
change. Do not edit generated SVG or introduce a second editable copy in draw.io.
The older installer draw.io overview is a different, dated view.

The views use stable Mermaid flowchart primitives, generic shapes and local
fonts. They require no remote icons, scripts, font downloads or production
Node.js dependency. [STACK.md](../STACK.md) records scope, source baselines,
notation, qualification gates and the evidence map.

## Render with existing local tools

The review environment used Mermaid CLI `11.16.0`, its standard Dagre layout
engine and an already installed Chrome browser. These are documentation tools,
not application runtime requirements. Do not install a renderer automatically.
Run from the Bridge root:

```sh
mmdc -i docs/diagrams/bridge-deployment.mmd -o docs/diagrams/bridge-deployment.svg -c docs/diagrams/mermaid-config.json -b white
mmdc -i docs/diagrams/k3s-components.mmd -o docs/diagrams/k3s-components.svg -c docs/diagrams/mermaid-config.json -b white
mmdc -i docs/diagrams/workload-data-paths.mmd -o docs/diagrams/workload-data-paths.svg -c docs/diagrams/mermaid-config.json -b white
```

If the existing renderer cannot locate Chrome, set
`PUPPETEER_EXECUTABLE_PATH` to the absolute path of an already installed compatible
browser. Do not bypass browser sandboxing or download a browser to hide that
prerequisite. Use the checked-in configuration to keep fonts, IDs and layout
consistent. Rendering can still differ between browser or CLI versions.

## Validate a change

1. Compare component placement and every labelled relationship with source.
   Update the source baseline in `STACK.md` only after that comparison.
2. Run all three rendering commands. A successful Markdown or XML parse does
   not prove that Mermaid accepts the source.
3. Open every SVG. For visual inspection as a bitmap, repeat its render command
   with a `.png` output in a new temporary directory and a suitable width such
   as `-w 1800`. Keep that review output separate from authoritative sources.
4. Check for clipping, crossed labels, ambiguous arrows, unreadable text and
   incorrect boundaries. Follow local documentation links and anchors.
5. Run `git diff --check` and, if the existing documentation audit tool is
   available, `ai-guardrails docs audit --path docs/STACK.md`. Review findings;
   a prose audit is not an architecture validator.

Structural, Mermaid-render and visual checks are separate results. None proves
that the host is installed, routes are reachable, GPUs transfer peer data, or
AI/gaming workloads are qualified. Do not run services or a cluster deployment
to regenerate documentation.
