import { Fragment, type ReactNode } from "react";

type Block =
  | { kind: "code"; lines: string[] }
  | { kind: "list"; ordered: boolean; lines: string[] }
  | { kind: "heading"; level: number; text: string }
  | { kind: "quote"; text: string }
  | { kind: "paragraph"; lines: string[] };

function blocks(source: string): Block[] {
  const result: Block[] = [];
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  for (let index = 0; index < lines.length; ) {
    const line = lines[index] ?? "";
    if (!line.trim()) {
      index += 1;
      continue;
    }
    if (line.trimStart().startsWith("```")) {
      const content: string[] = [];
      index += 1;
      while (index < lines.length && !(lines[index] ?? "").trimStart().startsWith("```")) {
        content.push(lines[index] ?? "");
        index += 1;
      }
      if (index < lines.length) index += 1;
      result.push({ kind: "code", lines: content });
      continue;
    }
    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    if (heading) {
      result.push({ kind: "heading", level: (heading[1] ?? "#").length, text: heading[2] ?? "" });
      index += 1;
      continue;
    }
    const bullet = /^\s*([-*]|\d+\.)\s+(.+)$/.exec(line);
    if (bullet) {
      const ordered = (bullet[1] ?? "").endsWith(".");
      const items: string[] = [];
      while (index < lines.length) {
        const match = /^\s*([-*]|\d+\.)\s+(.+)$/.exec(lines[index] ?? "");
        if (!match || (match[1] ?? "").endsWith(".") !== ordered) break;
        items.push(match[2] ?? "");
        index += 1;
      }
      result.push({ kind: "list", ordered, lines: items });
      continue;
    }
    if (line.startsWith("> ")) {
      result.push({ kind: "quote", text: line.slice(2) });
      index += 1;
      continue;
    }
    const paragraph = [line];
    index += 1;
    while (index < lines.length && (lines[index] ?? "").trim() && !/^(#{1,6})\s|^\s*([-*]|\d+\.)\s|^>\s|^```/.test(lines[index] ?? "")) {
      paragraph.push(lines[index] ?? "");
      index += 1;
    }
    result.push({ kind: "paragraph", lines: paragraph });
  }
  return result;
}

function inline(text: string): ReactNode[] {
  const parts = text.split(/(`[^`]+`|\*\*[^*]+\*\*)/g);
  return parts.map((part, index) => {
    if (part.startsWith("`") && part.endsWith("`"))
      return <code key={index}>{part.slice(1, -1)}</code>;
    if (part.startsWith("**") && part.endsWith("**"))
      return <strong key={index}>{part.slice(2, -2)}</strong>;
    return <Fragment key={index}>{part}</Fragment>;
  });
}

export function SafeMarkdown({ source }: { source: string }) {
  return (
    <article className="knowledge-markdown">
      {blocks(source).map((block, index) => {
        if (block.kind === "code")
          return <pre key={index}><code>{block.lines.join("\n")}</code></pre>;
        if (block.kind === "list") {
          const List = block.ordered ? "ol" : "ul";
          return <List key={index}>{block.lines.map((line, item) => <li key={item}>{inline(line)}</li>)}</List>;
        }
        if (block.kind === "heading") {
          const Heading = `h${block.level}` as "h1" | "h2" | "h3" | "h4" | "h5" | "h6";
          return <Heading key={index}>{inline(block.text)}</Heading>;
        }
        if (block.kind === "quote") return <blockquote key={index}>{inline(block.text)}</blockquote>;
        return <p key={index}>{block.lines.map((line, lineIndex) => <Fragment key={lineIndex}>{lineIndex ? <br /> : null}{inline(line)}</Fragment>)}</p>;
      })}
    </article>
  );
}
