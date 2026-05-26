import { format } from "@/utils/format";
import { Header } from "../components/Header";

const svc = await import("@/services/heavy-service");
const missing = await import("@/does-not-exist");

console.log(format, Header, svc, missing);
