import type { ReactNode } from "react";
import { GuideCaret, GuideClick, GuideCursor, GuideWindowBar } from "../guide/GuideParts";
import { ScreenIcon, type ScreenIconName } from "./screenIcons";
import css from "./ChatGPTConnectorScreens.module.css";

/** ChatGPT's words in the guide's ChatGPT screens (agentConnectorGuide:chatgpt.mock). */
export type ChatGPTWords = {
  address: string;
  newChat: string;
  images: string;
  plugins: string;
  settings: string;
  searchPlugins: string;
  addServer: string;
  name: string;
  connection: string;
  url: string;
  authentication: string;
  oauth: string;
  understand: string;
  create: string;
  /** The connector's name as typed, and the address pasted. */
  connectorName: string;
  mcpUrl: string;
};

/**
 * ChatGPT's page with its sidebar (seen signed out on chatgpt.com
 * 2026-11-08: 새 채팅, 이미지, 플러그인, 설정 and the plugins page's title and
 * search); `on` is the sidebar's chosen item.
 */
function ChatGPTPage({ W, on, children }: { W: ChatGPTWords; on: "chat" | "plugins"; children: ReactNode }) {
  const items: ReadonlyArray<[string, ScreenIconName, boolean]> = [
    [W.newChat, "compose", on === "chat"],
    [W.images, "image", false],
    [W.plugins, "at", on === "plugins"],
    [W.settings, "gear", false],
  ];
  return (
    <div className={css.chatgpt}>
      <GuideWindowBar title={on === "plugins" ? W.address : "chatgpt.com"} />
      <div className={css.app}>
        <div className={css.side}>
          <strong className={css.logo}>ChatGPT</strong>
          {items.map(([label, icon, chosen]) => (
            <span key={label} className={css.sideItem} data-on={chosen ? "" : undefined}>
              <ScreenIcon name={icon} />
              <span>{label}</span>
            </span>
          ))}
        </div>
        <div className={css.main}>{children}</div>
      </div>
    </div>
  );
}

function PluginsPage({ W, plus }: { W: ChatGPTWords; plus?: ReactNode }) {
  return (
    <>
      <span className={css.pluginsHead}>
        <strong className={css.title}>{W.plugins}</strong>
        <span className={css.search}>
          <ScreenIcon name="search" />
          <span className={css.faint}>{W.searchPlugins}</span>
        </span>
        <span className={css.plus} data-guide-target="plus">
          <ScreenIcon name="plus" />
          {plus}
        </span>
      </span>
      {/* The plugins themselves stand as the page's own loading shapes, not other companies' marks. */}
      <span className={css.cards} aria-hidden="true">
        {[0, 1, 2, 3, 4, 5].map((card) => (
          <span key={card} className={css.card}>
            <i className={css.cardIcon} />
            <i className={css.cardLines} />
          </span>
        ))}
      </span>
    </>
  );
}

/**
 * The guide's ChatGPT screens: Plugins › + › "Add custom MCP server", the
 * server's name, address and sign-in, and picking Leafen with @ in a chat.
 * The steps and words follow OpenAI's help (help.openai.com, "Connecting
 * custom MCP servers in ChatGPT", read 2026-11-08); the shapes follow
 * ChatGPT's pages seen signed out the same day.
 */
export function chatgptScreens(W: ChatGPTWords): Record<"open" | "address" | "done", ReactNode> {
  return {
    open: (
      <ChatGPTPage W={W} on="plugins">
        <PluginsPage W={W} plus={(
          <>
            <GuideClick at={1000} />
            <span className={css.menu}>
              <span className={css.menuItem} data-guide-target="addServer">
                {W.addServer}
                <GuideCursor
                  clickAt={2300}
                  path={[
                    { at: 0, on: "plus", x: -160, y: 120 },
                    { at: 850, on: "plus", x: 0, y: 0 },
                    { at: 1250, on: "plus", x: 0, y: 0 },
                    { at: 2100, x: 0, y: 0 },
                  ]}
                />
              </span>
            </span>
          </>
        )} />
      </ChatGPTPage>
    ),
    address: (
      <ChatGPTPage W={W} on="plugins">
        <PluginsPage W={W} />
        <span className={css.backdrop}>
          <span className={css.dialog}>
            <span className={css.dialogHead}>
              <strong>{W.addServer}</strong>
              <ScreenIcon name="close" />
            </span>
            <span className={css.label}>{W.name}</span>
            <span className={css.field}><span className={css.typedName}>{W.connectorName}</span></span>
            <span className={css.label}>{W.connection}</span>
            <span className={`${css.field} ${css.fieldFocus}`}>
              <span className={css.placeholder}>{W.url}</span>
              <span className={css.pasted}>{W.mcpUrl}</span>
              <GuideCaret />
            </span>
            <span className={css.label}>{W.authentication}</span>
            <span className={css.field}>
              {W.oauth}
              <span className={css.chevron}><ScreenIcon name="chevron" /></span>
            </span>
            <span className={css.check} data-guide-target="understand">
              <i className={css.box} aria-hidden="true"><ScreenIcon name="check" /></i>
              {W.understand}
              <GuideClick at={1900} />
            </span>
            <span className={css.footer}>
              <span className={css.primary} data-guide-target="create">
                {W.create}
                <GuideCursor
                  clickAt={3100}
                  path={[
                    { at: 0, on: "understand", x: 90, y: -70 },
                    { at: 800, on: "understand", x: 90, y: -70 },
                    { at: 1700, on: "understand", x: -60, y: 1 },
                    { at: 2050, on: "understand", x: -60, y: 1 },
                    { at: 2900, x: 0, y: 0 },
                  ]}
                />
              </span>
            </span>
          </span>
        </span>
      </ChatGPTPage>
    ),
    done: (
      <ChatGPTPage W={W} on="chat">
        <span className={css.chat}>
          <span className={css.mentions}>
            <span className={css.mention} data-guide-target="leafen">
              <i className={css.mentionIcon} aria-hidden="true"><ScreenIcon name="globe" /></i>
              {W.connectorName}
              <GuideCursor path={[{ at: 300, x: 140, y: 90 }, { at: 1300, x: 0, y: 0 }]} clickAt={1600} />
            </span>
          </span>
          <span className={css.composer}>
            <ScreenIcon name="plus" />
            <span className={css.typedAt}>
              @
              <GuideCaret />
            </span>
            <span className={css.send}><ScreenIcon name="arrowUp" /></span>
          </span>
        </span>
      </ChatGPTPage>
    ),
  };
}
