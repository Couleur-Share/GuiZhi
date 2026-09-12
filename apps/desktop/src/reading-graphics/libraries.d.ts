declare module "virtual:reading-libraries" {
  const value: { compiler: string; animation: string; components: Record<string, string>; runtime?: Record<string, string> };
  export default value;
}
