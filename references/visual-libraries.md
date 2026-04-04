# Visual Libraries for Lesson Content

Read this file when: spawning a visual-explainer agent for lesson content that involves equations, mathematical graphs, scientific diagrams, or domain-specific visualizations.

## How This Works

The study skill acts as a domain-aware dispatcher. When a lesson needs visual content, the skill determines *what type* of visual is needed and tells the visual-explainer agent *which libraries to use* in its prompt. Two rendering strategies:

1. **Web-native (inline)** — JS libraries loaded from CDN, rendered in the browser. Best for interactive content.
2. **SVG generation (pre-rendered)** — Python/CLI tools generate SVG files, embedded in the HTML via `<img>` tags. Best for domain-specific diagrams.

## Strategy Selection

When spawning a visual-explainer agent, include the appropriate library instructions based on content type:

| Content Type | Strategy | Library | CDN/Install |
|---|---|---|---|
| Equations | Web-native | KaTeX | CDN |
| 2D function plots | Web-native | JSXGraph | CDN |
| 3D surfaces/plots | Web-native | Plotly.js | CDN |
| Physics simulations | Web-native | p5.js + Matter.js | CDN |
| General diagrams | Web-native | Mermaid.js | CDN |
| Circuit diagrams | SVG generation | SchemDraw (Python) | pip |
| Molecular structures | SVG generation | RDKit (Python) | pip/conda |
| Star charts | SVG generation | Starplot (Python) | pip |
| Music notation | SVG generation | LilyPond | dnf |
| Vector fields/streamlines | SVG generation | Matplotlib (Python) | pip |
| Feynman diagrams | SVG generation | feynman (Python) | pip |
| Block/flow diagrams | SVG generation | blockdiag (Python) | pip |
| DNA/gene features | SVG generation | DNA Features Viewer (Python) | pip |
| Phylogenetic trees | SVG generation | phyTreeViz (Python) | pip |

## Web-Native Libraries (CDN)

### KaTeX — Equations

Use for any lesson with mathematical equations. LaTeX syntax, renders to HTML+CSS.

Tell the visual-explainer agent:
```
Include KaTeX for equation rendering. CDN setup:

<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16/dist/katex.min.css">
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16/dist/katex.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/katex@0.16/dist/contrib/auto-render.min.js"
  onload="renderMathInElement(document.body);"></script>

Use $...$ for inline math and $$...$$ for display math. LaTeX syntax.
```

### JSXGraph — 2D Mathematical Plotting

Use for function graphs, parametric curves, phase portraits, vector fields, interactive geometry.

```
Include JSXGraph for interactive 2D math plots. CDN:

<link href="https://cdn.jsdelivr.net/npm/jsxgraph/distrib/jsxgraph.css" rel="stylesheet">
<script src="https://cdn.jsdelivr.net/npm/jsxgraph/distrib/jsxgraphcore.js"></script>

JSXGraph is specifically designed for math education — supports sliders,
parametric curves, function plotting, differential equations visualization.
```

### Plotly.js — 3D Scientific Visualization

Use for 3D surfaces, contour maps, heatmaps, complex scientific plots.

```
Include Plotly.js for 3D visualization. CDN:

<script src="https://cdn.plot.ly/plotly-3.4.0.min.js"></script>

Use Plotly for 3D surface plots, contour maps, and scientific data visualization.
WebGL-accelerated, interactive rotation/zoom.
```

### p5.js + Matter.js — Physics Simulations

Use for interactive physics: pendulums, springs, waves, collisions, particles.

```
Include p5.js for rendering and Matter.js for physics. CDN:

<script src="https://cdnjs.cloudflare.com/ajax/libs/p5.js/1.11.1/p5.js"></script>
<script src="https://cdnjs.cloudflare.com/ajax/libs/matter-js/0.19.0/matter.min.js"></script>

p5.js handles drawing/animation, Matter.js handles rigid body physics.
For wave/field simulations, p5.js alone is sufficient (no Matter.js needed).
```

### Kekule.js — Interactive Molecular Structures

Use for chemistry lessons needing interactive molecule rendering.

```
Include Kekule.js for molecular visualization. CDN:

<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/kekule/dist/themes/default/kekule.css">
<script src="https://cdn.jsdelivr.net/npm/kekule/dist/kekule.min.js"></script>

MIT license. Supports 2D/3D structures, MOL/SDF/CML formats.
```

## SVG Generation Libraries (Python/CLI)

For these, the study skill generates a Python script, runs it via Bash, and the visual-explainer embeds the resulting SVG in the HTML page.

### Pattern for SVG Generation

```python
# Generate SVG in the practice directory
import subprocess
script = '''
import schemdraw
import schemdraw.elements as elm
# ... diagram code ...
d.save("practice/lesson-NN/diagrams/circuit.svg")
'''
# Write script, run with uv:
# uv run --with schemdraw python generate_diagram.py
```

Then tell visual-explainer: "Embed the SVG from practice/lesson-NN/diagrams/circuit.svg"

### SchemDraw — Circuit Diagrams

**Install:** `pip install schemdraw`
**LLM-friendly:** Excellent — simple imperative Python API.

```python
import schemdraw
import schemdraw.elements as elm

with schemdraw.Drawing(file='circuit.svg') as d:
    d += elm.SourceV().label('V1\n5V')
    d += elm.Resistor().right().label('R1\n1kΩ')
    d += elm.Capacitor().down().label('C1\n1μF')
    d += elm.Line().left()
    d += elm.Ground()
```

### Matplotlib — Vector Fields, Phase Portraits, General Scientific Plots

**Install:** `pip install matplotlib`
**LLM-friendly:** Excellent — most training data of any Python viz library.

Best for: streamlines, equipotential surfaces, phase portraits, electric field lines, custom scientific plots. SVG export via `fig.savefig('plot.svg')`.

### RDKit — Chemical Structures from SMILES

**Install:** `pip install rdkit` (or conda)
**LLM-friendly:** Good — SMILES strings are text-based molecular descriptions.

```python
from rdkit import Chem
from rdkit.Chem import Draw

mol = Chem.MolFromSmiles('CC(=O)OC1=CC=CC=C1C(=O)O')  # Aspirin
Draw.MolToFile(mol, 'aspirin.svg', size=(300, 300))
```

### Starplot — Astronomy Star Charts

**Install:** `pip install starplot` (Python 3.10+)
**LLM-friendly:** Good — clean API, well-documented.

Produces star charts, sky maps, and celestial visualizations as SVG via matplotlib backend.

### LilyPond — Music Notation

**Install:** `dnf install lilypond`
**LLM-friendly:** Good — text-based DSL similar to LaTeX.

```lilypond
\relative c' { c4 d e f | g2 g | a4 a a a | g1 | }
```
Run: `lilypond --svg -o output score.ly`

### DNA Features Viewer — Gene Diagrams

**Install:** `pip install dna_features_viewer`
**LLM-friendly:** Good — declarative Python API.

### phyTreeViz — Phylogenetic Trees

**Install:** `pip install phytreeviz`
**LLM-friendly:** Good — simple CLI with SVG output.

### blockdiag — System Block Diagrams

**Install:** `pip install blockdiag`
**LLM-friendly:** Excellent — text DSL similar to DOT/Graphviz.

```
blockdiag { A -> B -> C; B -> D; }
```
Run: `blockdiag -Tsvg -o diagram.svg input.diag`

## Combining in a Single Page

A lesson page can use multiple libraries. Common combinations:

- **Physics lesson:** KaTeX (equations) + JSXGraph (function plots) + p5.js (simulation)
- **Chemistry lesson:** KaTeX (reaction equations) + Kekule.js (molecules) + embedded RDKit SVGs
- **EE lesson:** KaTeX (circuit equations) + embedded SchemDraw SVGs
- **Biology lesson:** Mermaid (pathway diagrams) + embedded DNA Features Viewer SVGs
- **Music theory:** KaTeX (interval math) + embedded LilyPond SVGs
- **Astronomy:** KaTeX (orbital mechanics) + embedded Starplot SVGs

## Graceful Degradation

If a Python library isn't installed, the skill should:
1. Note it in the lesson: "Install schemdraw for circuit diagrams: pip install schemdraw"
2. Fall back to ASCII/text representation of the diagram
3. Never fail the lesson because a visualization library is missing
