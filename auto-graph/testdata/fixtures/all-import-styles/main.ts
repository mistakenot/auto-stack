import { foo } from "./static_target";
import("./dynamic_target");
const bar = require("./require_target");
import "./side_effect";
import type { MyType } from "./type_target";

console.log(foo, bar);
