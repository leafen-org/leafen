import { useEffect, useState } from "react";
import { i18next } from "@/i18n";
import { fetchRuntimeConfig, subscribeRuntimeConfig } from "./runtimeConfigService";

// A server's name (internal/app/server_name.go): its administrators' name,
// or the friendly alias every server gets when it is made, shown in the
// person's language.

export type ServerNaming = { name: string; alias: string };

/** The server's name as the person reads it, "" when it says none. */
export function serverDisplayName(server: ServerNaming): string {
  if (server.name) return server.name;
  if (!server.alias) return "";
  const english = server.alias.charAt(0).toUpperCase() + server.alias.slice(1);
  return String(i18next.t(`common:serverAliases.${server.alias}`, { defaultValue: english }));
}

/** This server's name, from its runtime settings; null until they are read. */
export function useServerNaming(): ServerNaming | null {
  const [naming, setNaming] = useState<ServerNaming | null>(null);
  useEffect(() => {
    let live = true;
    void fetchRuntimeConfig().then(({ config }) => {
      if (live) setNaming({ name: config.serverName ?? "", alias: config.serverAlias ?? "" });
    });
    const stop = subscribeRuntimeConfig((config) => setNaming({ name: config.serverName ?? "", alias: config.serverAlias ?? "" }));
    return () => {
      live = false;
      stop();
    };
  }, []);
  return naming;
}
