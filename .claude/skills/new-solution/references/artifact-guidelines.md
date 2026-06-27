# Artifact Guidelines

Artifacts are self-contained `.html` files stored in the task folder (or an `artifacts/` subfolder) and linked from plan.md. They are created during planning.

## When to Create Artifacts

- **Requirements stage**: user flow diagrams (if the feature involves user-facing flows)
- **Solution stage**: technical diagrams (architecture, sequence), wireframes

## Wireframes

Structural layouts, not polished UI. Show where elements go -- buttons, sections, headings -- without real content. Each element should just have a label describing what it is.

- Self-contained HTML page using the project's CSS framework CDN (e.g. Bootstrap, Tailwind)
- Structural only: grey boxes, placeholder labels, layout grid. No real content or styling beyond framework defaults.
- Match the project's UI framework so wireframes roughly reflect actual component structure
- One wireframe per `.html` file, linked from plan.md

## Diagrams (Mermaid)

Self-contained HTML files that render Mermaid diagrams via beautiful-mermaid. Use for flow diagrams, sequence diagrams, ERDs, etc.

- Use `renderMermaidSVG` from `esm.sh/beautiful-mermaid@1.1.3`
- OK to add short explanation text below the diagram (bullets work well)
- Split any diagram with more than 8 nodes into multiple diagrams
- One diagram per `.html` file, linked from plan.md

**Boilerplate:**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title>[Diagram Name]</title>
  </head>
  <body>
    <div id="diagram"></div>
    <ul>
      <li>Brief note about the diagram</li>
    </ul>
    <script type="module">
      import { renderMermaidSVG } from 'https://esm.sh/beautiful-mermaid@1.1.3';

      const svg = await renderMermaidSVG(`graph TD
    A[Start] --> B[Step]
    B --> C[End]`);

      document.getElementById('diagram').innerHTML = svg;
    </script>
  </body>
</html>
```

## Linking Artifacts

Reference artifacts from plan.md Links section and from solution.md where relevant:

```markdown
## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)
- [auth-flow.html](./auth-flow.html)
- [settings-wireframe.html](./settings-wireframe.html)
```
