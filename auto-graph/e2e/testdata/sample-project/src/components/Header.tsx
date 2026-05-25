import { formatDate } from "@utils/format";

export class Header {
  render() {
    return `Header: ${formatDate(new Date())}`;
  }
}
