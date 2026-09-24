import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import { NextIntlClientProvider } from "next-intl";
import "./globals.css";
import Providers from "./providers";
import { Analytics } from "@vercel/analytics/next";
import { SpeedInsights } from "@vercel/speed-insights/next";
import { cn } from "@/lib/utils";
import enMessages from "../messages/en.json";
import { THEME_BOOTSTRAP_SCRIPT } from "@/lib/theme";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-sans",
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
});

export const metadata: Metadata = {
  title: "TrusTrove | Decentralized Trade Finance Operations Terminal",
  description:
    "Tokenize unpaid trade invoices as Stellar assets and receive immediate USDC funding. Yield opportunities for liquidity providers.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    // `suppressHydrationWarning` is required because the bootstrap script below
    // may swap the `dark` class before React hydrates (see lib/theme.ts).
    <html lang="en" className="dark" suppressHydrationWarning>
      <head>
        <script
          // Runs before first paint so a saved (or system) light preference is
          // applied without a flash of the server-rendered dark theme.
          dangerouslySetInnerHTML={{ __html: THEME_BOOTSTRAP_SCRIPT }}
        />
      </head>
      <body
        className={`${inter.variable} ${jetbrainsMono.variable} antialiased bg-background text-foreground font-sans min-h-screen`}
      >
        <a
          href="#main-content"
          className={cn(
            "sr-only focus:not-sr-only focus:absolute focus:top-4 focus:left-4 focus:z-[100]",
            "focus:px-4 focus:py-2 focus:bg-primary focus:text-black focus:font-bold focus:text-sm focus:rounded",
            "focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-background",
          )}
        >
          Skip to main content
        </a>
        <Providers>
          <NextIntlClientProvider locale="en" messages={enMessages}>
            {children}
          </NextIntlClientProvider>
          <SpeedInsights />
        </Providers>
        <Analytics />
      </body>
    </html>
  );
}
