// A plugin run by runPlugin over stdio, for the helper's subprocess tests.
// The last argument chooses its behavior.
import { runPlugin } from "../../kit-plugin.ts";

const mode = process.argv.at(-1);

runPlugin(async (kit) => {
  if (mode === "fail") throw new Error("setup failed on purpose");
  console.log("console.log from setup");
  console.info("console.info from setup");
  process.stdout.write("process.stdout.write from setup\n");
  await kit.commands.register({
    id: "shout",
    description: "Log loudly",
    execute: (args) => {
      console.log(`shouting ${args}`);
    },
  });
  if (mode === "reject") void Promise.reject(new Error("nobody caught this"));
  kit.ui.toast({ title: "ready" });
  return () => {
    process.stderr.write("disposed\n");
  };
});
