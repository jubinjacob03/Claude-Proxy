import { Outfit, Newsreader } from "next/font/google";
import "./globals.css";
import ToastProvider from "@/components/toast/ToastProvider";

const outfit = Outfit({
  subsets: ["latin"],
  variable: "--font-outfit",
  display: "swap",
});

const newsreader = Newsreader({
  subsets: ["latin"],
  variable: "--font-newsreader",
  display: "swap",
  style: ["normal", "italic"],
});

export const metadata = {
  title: "Claude-Proxy Licensing",
  description: "Issue licences, manage the API key pool, and watch usage.",
};

export default function RootLayout({ children }) {
  return (
    <html lang="en" className={`${outfit.variable} ${newsreader.variable}`}>
      <body className="min-h-screen font-sans antialiased text-white/90">
        <ToastProvider>{children}</ToastProvider>
      </body>
    </html>
  );
}
