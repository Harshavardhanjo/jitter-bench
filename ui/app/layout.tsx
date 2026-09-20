import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "jitter-bench",
  description:
    "The latency a jitter buffer must add to keep audio continuous, measured in the browser by the same Go code as the command line tool.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="bg-neutral-50 text-neutral-900 antialiased dark:bg-neutral-950 dark:text-neutral-100">
        {children}
      </body>
    </html>
  );
}
