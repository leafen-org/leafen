import type { CSSProperties, ReactNode } from "react";
import { GuideCursor, GuideWindowBar } from "@/components/guide/GuideParts";
import { CODE_EXCHANGE_PATH } from "@/lib/agentConnectionText";
import { guideKeyCap } from "@/lib/menuKeyHint";
import { serverUrl } from "@/lib/serverOrigin";
import s from "./AgentPasteGuide.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/** What the agent paste guide's screens say, from the sources the real screens read. */
export type AgentPasteGuideWords = {
  /** Which of Leafen's screens the guide stands on: the first run's step, or Settings › Agents. */
  place: "firstRun" | "settings";
  leafenWindow: string;
  stepTitle: string;
  stepBody: string;
  copy: string;
  more: string;
  later: string;
  copied: string;
  /** "Connected: Claude Code", as the step says it. */
  connected: string;
  connectedBody: string;
  continueLabel: string;
  navAgents: string;
  settingsLead: string;
  pasteHint: string;
  connectedListTitle: string;
  agentStart: string;
  agentDone: string;
  /** The text being copied: its first lines show as the agent got them. */
  prompt: string;
};

/** The name an agent gives itself as it trades the code (agentPrompt:nameHint's example). */
export const GUIDE_AGENT = "Claude Code";
const AGENT = GUIDE_AGENT;

const at = (ms: number) => ({ "--at": `${ms}ms` }) as CSSProperties;
const between = (from: number, until: number) => ({ "--at": `${from}ms`, "--until": `${until}ms` }) as CSSProperties;
const until = (ms: number) => ({ "--until": `${ms}ms` }) as CSSProperties;

/** Where the text says agents reach Leafen: its first MCP address, or the active server's. */
function mcpAddress(prompt: string): string {
  return prompt.match(/https?:\/\/\S+\/api\/leafen\/mcp/u)?.[0] ?? serverUrl("/api/leafen/mcp");
}

/**
 * The guide's four screens: Leafen's [Copy text], the text pasted into Claude
 * Code and sent, Claude Code asking to run what adds Leafen, and Leafen saying
 * it connected. Leafen's screens are its own, drawn as they are where the
 * guide stands; Claude Code's are as it draws itself (the guide's stylesheet
 * says when they were seen), its long output cut short.
 */
export function agentPasteGuideScreens(W: AgentPasteGuideWords): ReactNode[] {
  const lines = W.prompt.split("\n");
  // Claude Code folds a long paste to "[Pasted text #1 +n lines]", n its line breaks.
  const pasted = `[Pasted text #1 +${lines.length - 1} lines]`;
  const mcp = mcpAddress(W.prompt);
  const exchange = mcp.replace(/\/api\/leafen\/mcp$/u, CODE_EXCHANGE_PATH);
  // The text's one command for Claude Code (lib/agentConnection.ts), its head
  // as Claude Code shows a long one: it looks at the connections Claude Code
  // lists at this Leafen first, then takes a name it doesn't hold yet,
  // trades the code and adds Leafen under it.
  const command = `C="\${CLAUDE_CODE_EXECPATH:-claude}"; S=user; W=""; R=""; for M in $("$C" mcp list 2>/dev/null | …`;
  const sent = (
    <div className={s.sent}>
      <span className={s.line}>{`❯ ${lines[0] ?? ""}`}</span>
      <span className={s.line}>{`  ${lines[1] ?? ""}`}</span>
      <span className={`${s.line} ${s.dim}`}>  …</span>
    </div>
  );
  const settings = W.place === "settings";
  return [
    settings ? settingsScreen(W, false) : stepScreen(W, false),
    <>
      <GuideWindowBar title="~ — claude" />
      <div className={s.terminal}>
        <div className={s.welcome}>
          <span className={s.line}>{" ▐▛███▜▌"}</span>
          <span className={`${s.line} ${s.bold}`}>Claude Code</span>
          <span className={s.line}>{"▝▜█████▛▘"}</span>
          <span className={`${s.line} ${s.dim}`}>~/projects</span>
          <span className={s.line}>{"  ▘▘ ▝▝"}</span>
          <span />
        </div>
        <div className={s.gap} />
        <div className={s.appear} style={at(2300)}>
          {sent}
          <div className={s.gap} />
          <span className={`${s.line} ${s.claude}`}>✢ Envisioning…</span>
          <div className={s.gap} />
        </div>
        <div className={s.rule} />
        <div className={s.stack}>
          <span className={`${s.line} ${s.vanish}`} style={until(1000)}>❯ </span>
          <span className={`${s.line} ${s.flash}`} style={between(1000, 2300)}>{`❯ ${pasted}`}</span>
          <span className={`${s.line} ${s.appear}`} style={at(2300)}>❯ </span>
        </div>
        <div className={s.rule} />
        <div className={s.stack}>
          <span className={`${s.line} ${s.dim} ${s.vanish}`} style={until(1000)}>  ? for shortcuts</span>
          <span className={`${s.line} ${s.dim} ${s.flash}`} style={between(1000, 2300)}>  paste again to expand</span>
          <span className={`${s.line} ${s.dim} ${s.appear}`} style={at(2300)}>  esc to interrupt</span>
        </div>
        <span className={`${s.keycap} ${s.flash}`} style={{ ...between(600, 1300), top: 62 }}>{guideKeyCap("⌘V")}</span>
        <span className={`${s.keycap} ${s.flash}`} style={{ ...between(1800, 2400), top: 62 }}>{guideKeyCap("↵")}</span>
      </div>
    </>,
    <>
      <GuideWindowBar title="~ — claude" />
      <div className={s.terminal}>
        {sent}
        <div className={s.gap} />
        <span className={s.wrap}>{`⏺ ${W.agentStart}`}</span>
        <div className={s.gap} />
        <div className={s.stack}>
          <span className={`${s.line} ${s.vanish}`} style={until(1600)}>
            <span className={s.dim}>⏺</span>{` Bash(${command})`}
          </span>
          <span className={`${s.line} ${s.appear}`} style={at(1600)}>
            <span className={s.ok}>⏺</span>{` Bash(${command})`}
          </span>
        </div>
        <div className={s.stack}>
          <div className={s.vanish} style={until(1600)}>
            <span className={`${s.line} ${s.dim}`}>  ⎿  Waiting…</span>
            <div className={s.gap} />
            <div className={`${s.rule} ${s.askRule}`} />
            <span className={`${s.line} ${s.ask} ${s.bold}`}> Bash command</span>
            <span className={`${s.line} ${s.quoted}`}>{command}</span>
            <span className={`${s.line} ${s.quoted}`}>{`else E="$(curl -sSf '${exchange}' …)" && "$C" mcp add-json --scope "$S" "$N" "$E" && …; fi; fi`}</span>
            <span className={`${s.line} ${s.dim}`}> Contains expansion</span>
            <span className={s.line}> Do you want to proceed?</span>
            <span className={`${s.line} ${s.ask}`}> ❯ 1. Yes</span>
            <span className={`${s.line} ${s.dim}`}>   2. Yes, and switch to auto mode</span>
            <span className={`${s.line} ${s.dim}`}>   3. No</span>
            <div className={s.gap} />
            <span className={`${s.line} ${s.dim}`}> Esc to cancel · Tab to amend</span>
          </div>
          <div className={s.appear} style={at(1600)}>
            {/* One command trades the code and adds Leafen: the key shows nowhere. */}
            <span className={s.line}><span className={s.dim}>  ⎿  </span>Added http MCP server leafen to user config</span>
            <div className={s.gap} />
            <div className={s.appear} style={at(2400)}>
              <span className={s.wrap}>{`⏺ ${W.agentDone}`}</span>
            </div>
          </div>
        </div>
        <span className={`${s.keycap} ${s.flash}`} style={{ ...between(1000, 1600), top: 164 }}>{guideKeyCap("↵")}</span>
      </div>
    </>,
    settings ? settingsScreen(W, true) : stepScreen(W, true),
  ];
}

/** The first run's agents step: copying the text, or the agent connected. */
function stepScreen(W: AgentPasteGuideWords, connected: boolean): ReactNode {
  return (
    <>
      <GuideWindowBar title={W.leafenWindow} />
      <div className={s.leafen}>
        <div className={s.panel}>
          <span className={s.brand}>
            <img src={pageMarkSource()} alt="" />
            Leafen
          </span>
          {connected ? (
            <>
              <strong className={`${s.title} ${s.fadeIn}`} style={at(300)}>{W.connected}</strong>
              <span className={`${s.body} ${s.fadeIn}`} style={at(300)}>{W.connectedBody}</span>
              <span className={`${s.copyWide} ${s.fadeIn}`} style={at(300)}>{W.continueLabel}</span>
            </>
          ) : (
            <>
              <strong className={s.title}>{W.stepTitle}</strong>
              <span className={s.body}>{W.stepBody}</span>
              <span className={s.copyWide}>
                {W.copy}
                <GuideCursor path={[{ at: 300, x: 120, y: 90 }, { at: 1200, x: 0, y: 0 }]} clickAt={1500} />
              </span>
              <span className={s.fold}>{`▸ ${W.more}`}</span>
              <span className={s.later}>{W.later}</span>
            </>
          )}
        </div>
        {connected ? null : <span className={`${s.toast} ${s.fadeIn}`} style={at(1700)}>{W.copied}</span>}
      </div>
    </>
  );
}

/** Settings › Agents: copying the text, or the agent connected and listed. */
function settingsScreen(W: AgentPasteGuideWords, connected: boolean): ReactNode {
  return (
    <>
      <GuideWindowBar title={W.leafenWindow} />
      <div className={s.settings}>
        <div className={s.settingsNav} aria-hidden="true">
          <i />
          <i />
          <i />
          <span>{W.navAgents}</span>
          <i />
          <i />
        </div>
        <div className={s.settingsMain}>
          <strong className={s.settingsTitle}>{W.navAgents}</strong>
          <span className={s.settingsLead}>{W.settingsLead}</span>
          <div className={s.settingsRow}>
            <span className={s.settingsHint}>{W.pasteHint}</span>
            <span className={s.copyBlue}>
              {W.copy}
              {connected ? null : <GuideCursor path={[{ at: 300, x: -90, y: 100 }, { at: 1200, x: 0, y: 0 }]} clickAt={1500} />}
            </span>
          </div>
          {connected ? (
            <>
              <div className={`${s.connectedNow} ${s.fadeIn}`} style={at(300)}>
                <strong>{W.connected}</strong>
                <span>{W.connectedBody}</span>
              </div>
              <strong className={s.listTitle}>{W.connectedListTitle}</strong>
              <div className={`${s.listRow} ${s.fadeIn}`} style={at(300)}>
                <strong>{AGENT}</strong>
              </div>
            </>
          ) : (
            <span className={s.settingsHint}>{`▸ ${W.more}`}</span>
          )}
        </div>
        {connected ? null : <span className={`${s.toast} ${s.fadeIn}`} style={at(1700)}>{W.copied}</span>}
      </div>
    </>
  );
}
