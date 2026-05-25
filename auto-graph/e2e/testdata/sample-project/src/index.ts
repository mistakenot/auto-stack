import { App } from "./components/App";
import { formatDate } from "@utils/format";
import { UserService } from "./services/userService";

const app = new App();
const date = formatDate(new Date());
const svc = new UserService();
console.log(app, date, svc);
