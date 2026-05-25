// Pure type file - no runtime imports (leaf node)
export interface User {
  id: string;
  name: string;
  email: string;
}

export interface UserProfile extends User {
  bio: string;
}
