/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static export. The simulation runs in the browser through WebAssembly, so
  // there is no server to deploy and nothing to keep running: the whole UI is
  // files. That is also the honest architecture for a benchmark, since a hosted
  // backend would let the numbers depend on a machine the reader cannot see.
  output: "export",
  reactStrictMode: true,
};

export default nextConfig;
