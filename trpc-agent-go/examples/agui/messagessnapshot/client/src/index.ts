import { HttpAgent } from "@ag-ui/client";
import { v4 as uuidv4 } from "uuid";

async function main() {
  const chatUrl = process.env.AG_UI_ENDPOINT ?? "http://127.0.0.1:8080/agui";
  const historyUrl = process.env.AG_UI_HISTORY_ENDPOINT ?? "http://127.0.0.1:8080/history";
  const userId = process.env.AG_UI_USER_ID ?? "demo-user";
  const prompt = process.env.AG_UI_PROMPT ?? "请帮我计算2*(10+11)，并解释计算过程，然后给出最终结论。";
  const threadId = process.env.AG_UI_THREAD_ID ?? `thread-${Date.now()}`;

  const chatAgent = new HttpAgent({
    url: chatUrl,
    threadId,
    initialMessages: [
      {
        id: uuidv4(),
        role: "user",
        content: prompt,
      },
    ],
  });

  console.log(`⚙️ Send chat request to -> ${chatUrl}`);
  const chatResult = await chatAgent.runAgent({
    forwardedProps: {
      userId,
    },
  });

  chatResult.newMessages.forEach((message) => {
    if (message.role === "assistant") {
      console.log(`🤖 assistant: ${message.content ?? ""}`);
    }
    if (message.role === "tool") {
      console.log(
        `🛠️ tool(${message.toolCallId ?? "unknown"}): ${message.content ?? ""}`,
      );
    }
  });

  const historyAgent = new HttpAgent({
    url: historyUrl,
    threadId,
  });

  console.log(`⚙️ Load history -> ${historyUrl}`);
  await historyAgent.runAgent({
    forwardedProps: {
      userId,
    },
  });

  historyAgent.messages.forEach((message) => {
    if (message.role === "assistant") {
      console.log(`🤖 assistant: ${message.content ?? ""}`);
      return;
    }
    if (message.role === "tool") {
      console.log(
        `🛠️ tool(${message.toolCallId ?? "unknown"}): ${message.content ?? ""}`,
      );
      return;
    }
    if (message.role === "user") {
      const sender = message.name ?? userId;
      console.log(`👤 user(${sender}): ${message.content ?? ""}`);
      return;
    }
    console.log(`❓ ${message.role}: ${message.content ?? ""}`);
  });

  console.log(`threadId=${threadId}, userId=${userId}`);
}

main().catch((error) => {
  console.error("Failed to run:", error);
  process.exit(1);
});