/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: {
          base:  "#0a0a0f",
          panel: "#11111a",
        },
        cy:  "#00f0ff",
        yl:  "#fcee0a",
        rd:  "#ff003c",
        dim: "#6b7280",
        txt: "#e6f9ff",
      },
      fontFamily: {
        hud:  ["'Share Tech Mono'", "monospace"],
        mono: ["'JetBrains Mono'", "monospace"],
      },
      keyframes: {
        pulseDot: {
          "0%,100%": { opacity: 1 },
          "50%":     { opacity: 0.3 },
        },
        glitch: {
          "0%,100%": { transform: "translate(0,0)" },
          "20%":     { transform: "translate(-1px,1px)" },
          "40%":     { transform: "translate(1px,-1px)" },
        },
      },
      animation: {
        pulseDot: "pulseDot 1.2s ease-in-out infinite",
        glitch:   "glitch 0.4s steps(2,end) 1",
      },
    },
  },
  plugins: [],
};
