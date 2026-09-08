import { useState } from "react";
import { About } from "./screens/About";
import { Shell } from "./screens/Shell";

type Screen = "shell" | "about";

export default function App() {
  const [screen, setScreen] = useState<Screen>("shell");

  // Iteración 0 no tiene router: son dos pantallas y no hay URLs que preservar.
  // Cuando aparezcan las tabs de documentos, la navegación vive en el estado
  // del workspace, no en el historial del webview.
  return screen === "about" ? (
    <About onBack={() => setScreen("shell")} />
  ) : (
    <Shell onOpenAbout={() => setScreen("about")} />
  );
}
