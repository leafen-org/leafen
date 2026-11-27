// The Hello app's screen: connect to Leafen, show what it says, run the
// greet action and keep the theme and the app's place in step.
import { connect } from "./leafen-app-sdk.js";

const log = (text) => {
  const item = document.createElement("li");
  item.textContent = text;
  document.getElementById("log").append(item);
};

const leafen = await connect();
document.documentElement.dataset.theme = leafen.context.theme;
leafen.onTheme((theme) => { document.documentElement.dataset.theme = theme; });
leafen.onLocation((path) => log(`place: /${path}`));
log(`language: ${leafen.context.locale}, theme: ${leafen.context.theme}, version: ${leafen.context.app.version}`);

// The person's own profile: a read the app's personAccess "read" allows.
const me = await leafen.api("GET", "/account/profile");
log(`api /account/profile: ${me.status}`);
if (me.status === 200) document.getElementById("name").value = me.body.displayName;

document.getElementById("greet").addEventListener("submit", async (event) => {
  event.preventDefault();
  const name = document.getElementById("name").value || "there";
  try {
    const { output } = await leafen.action("greet", { name });
    document.getElementById("answer").textContent = `${output.message} (greeting ${output.count})`;
    await leafen.navigate(`greeted/${encodeURIComponent(name)}`);
    await leafen.notice(output.message);
  } catch (failure) {
    document.getElementById("answer").textContent = `${failure.code}: ${failure.message}`;
  }
});
