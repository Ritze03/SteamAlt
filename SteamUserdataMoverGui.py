import os
import re
import shutil
import tkinter as tk
from tkinter import ttk, messagebox, filedialog
from PIL import Image, ImageTk
import urllib.request
import io
from urllib.error import URLError
import psutil

# Initialize STEAM_USERDATA_PATH as empty
STEAM_USERDATA_PATH = ""

# First, try to find the default Steam userdata path
default_paths = [
    r"C:\Program Files (x86)\Steam\userdata",
    os.path.expanduser(r"~\AppData\Local\Steam\userdata"),
    os.path.expanduser(r"~\.steam\steam\userdata"),
    os.path.expanduser(r"~/.local/share/Steam/userdata"),
    os.path.expanduser(r"~/.var/app/com.valvesoftware.Steam/.local/share/Steam/userdata"),
    os.path.expanduser(r"~/.var/app/com.valvesoftware.Steam/data/Steam/userdata")
]

for path in default_paths:
    if os.path.exists(path):
        STEAM_USERDATA_PATH = path
        break

AVATAR_BASE_URL = "https://avatars.cloudflare.steamstatic.com/{}.jpg"
PLACEHOLDER_IMAGE_URL = "https://images.steamusercontent.com/ugc/868480752636433334/1D2881C5C9B3AD28A1D8852903A8F9E1FF45C2C8/"

def select_steam_userdata_path():
    """Open a folder selector dialog to manually select the Steam userdata path."""
    root = tk.Tk()
    root.withdraw()  # Hide the main window
    
    messagebox.showinfo(
        "Steam Userdata Path Not Found",
        "The Steam userdata folder was not found automatically. Please select it manually."
    )
    
    path = filedialog.askdirectory(
        title="Select Steam userdata folder",
        initialdir=os.path.expanduser("~")
    )
    
    root.destroy()
    
    if path and os.path.exists(os.path.join(path, "..", "steam.exe")):
        return path
    return None

def is_steam_running():
    """Check if Steam client is currently running."""
    for proc in psutil.process_iter(['name']):
        if proc.info['name'].lower() == 'steam.exe':
            return True
    return False

def close_steam():
    """Attempt to close the Steam client."""
    try:
        for proc in psutil.process_iter(['name']):
            if proc.info['name'].lower() == 'steam.exe':
                proc.terminate()
                proc.wait(timeout=10)  # Wait up to 10 seconds for process to end
        return True
    except Exception as e:
        print(f"Error closing Steam: {e}")
        return False

def extract_user_data(steamid3):
    config_path = os.path.join(STEAM_USERDATA_PATH, steamid3, "config", "localconfig.vdf")
    username = "(unknown)"
    avatar_hash = None

    if not os.path.exists(config_path):
        print(f"Error: File does not exist: {config_path}")
        return (steamid3, username, avatar_hash)

    with open(config_path, 'r', encoding='utf-8') as f:
        content = f.read()

    print(f"Debug: Reading localconfig.vdf for SteamID3 {steamid3}...")

    # New approach: Find the SteamID3 block by counting braces
    start_pattern = rf'"{steamid3}"\s*{{'
    start_match = re.search(start_pattern, content)
    
    if start_match:
        start_pos = start_match.end()
        brace_count = 1
        current_pos = start_pos
        
        # Find the matching closing brace
        while brace_count > 0 and current_pos < len(content):
            if content[current_pos] == '{':
                brace_count += 1
            elif content[current_pos] == '}':
                brace_count -= 1
            current_pos += 1
        
        if brace_count == 0:
            block_content = content[start_pos:current_pos-1]
            print(f"Debug: Found complete SteamID3 block:\n{block_content}")

            # Extract username
            name_match = re.search(r'"NameHistory"\s*{\s*"0"\s*"(.*?)"', block_content)
            if name_match:
                username = name_match.group(1)
                print(f"Debug: Found username: {username}")

            # Extract avatar hash
            avatar_match = re.search(r'"avatar"\s*"([a-f0-9]+)"', block_content)
            if avatar_match:
                avatar_hash = avatar_match.group(1)
                print(f"Debug: Found avatar hash: {avatar_hash}")
            else:
                print("Debug: Avatar hash not found in block")
        else:
            print("Debug: Could not find matching closing brace")
    else:
        print(f"Debug: No match found for SteamID3 {steamid3}")

    print(f"Extracted User: {username}, SteamID3: {steamid3}, Avatar Hash: {avatar_hash}")
    return (steamid3, username, avatar_hash)

def get_user_list():
    users = []
    if not os.path.exists(STEAM_USERDATA_PATH):
        return users
        
    for folder in os.listdir(STEAM_USERDATA_PATH):
        if folder.isdigit():
            print(f"\nDebug: Processing folder for SteamID3: {folder}")
            users.append(extract_user_data(folder))
    return users

def download_avatar(steamid3):
    if not steamid3:
        return download_placeholder()
    # Construct the direct URL using SteamID3
    url = f"https://avatars.cloudflare.steamstatic.com/{steamid3}_full.jpg"  # Direct URL with SteamID3
    try:
        with urllib.request.urlopen(url) as u:
            raw_data = u.read()
        im = Image.open(io.BytesIO(raw_data)).resize((64, 64))  # Resize image to fit
        return ImageTk.PhotoImage(im)
    except (URLError, IOError):
        return download_placeholder()

def download_placeholder():
    try:
        with urllib.request.urlopen(PLACEHOLDER_IMAGE_URL) as u:
            raw_data = u.read()
        im = Image.open(io.BytesIO(raw_data)).resize((64, 64))  # Resize image to fit
        return ImageTk.PhotoImage(im)
    except (URLError, IOError):
        # Fallback to a blank image if placeholder can't be downloaded
        im = Image.new('RGB', (64, 64), color='gray')
        return ImageTk.PhotoImage(im)

def copy_numeric_folders(src_id, dst_id):
    # Check if Steam is running
    if is_steam_running():
        if not messagebox.askyesno(
            "Steam is Running",
            "The Steam client is currently running. It's recommended to close Steam before copying userdata.\n"
            "Do you want to close Steam now?",
            icon='warning'
        ):
            if not messagebox.askyesno(
                "Continue Anyway?",
                "Copying userdata while Steam is running may cause problems.\n"
                "Are you sure you want to continue without closing Steam?",
                icon='warning'
            ):
                return False
        else:
            if not close_steam():
                messagebox.showerror(
                    "Error",
                    "Failed to close Steam. Please close it manually and try again."
                )
                return False

    src_path = os.path.join(STEAM_USERDATA_PATH, src_id)
    dst_path = os.path.join(STEAM_USERDATA_PATH, dst_id)

    if not os.path.exists(src_path):
        raise FileNotFoundError(f"Source path does not exist: {src_path}")
    if not os.path.exists(dst_path):
        os.makedirs(dst_path)

    for item in os.listdir(src_path):
        if not item.isdigit():
            continue
        src_item = os.path.join(src_path, item)
        dst_item = os.path.join(dst_path, item)
        if os.path.exists(dst_item):
            shutil.rmtree(dst_item)
        shutil.copytree(src_item, dst_item)
    return True

# ---------------- UI ----------------

class SteamCopierApp:
    def __init__(self, root):
        self.root = root
        self.root.title("SteamAlt - Userdata Copier")

        # Check if we have a valid STEAM_USERDATA_PATH
        global STEAM_USERDATA_PATH
        if not STEAM_USERDATA_PATH or not os.path.exists(STEAM_USERDATA_PATH):
            new_path = select_steam_userdata_path()
            if new_path:
                STEAM_USERDATA_PATH = new_path
            else:
                messagebox.showerror(
                    "Error",
                    "No valid Steam userdata path selected. Application will exit."
                )
                root.destroy()
                return

        self.users = get_user_list()
        if not self.users:
            messagebox.showerror(
                "Error",
                f"No user accounts found in:\n{STEAM_USERDATA_PATH}\n\n"
                "Please verify the Steam userdata path and try again."
            )
            root.destroy()
            return

        self.avatars = {}

        self.src_var = tk.StringVar()
        self.dst_var = tk.StringVar()

        # Initialize image references
        self.avatar_src_image = None
        self.avatar_dst_image = None

        self.create_widgets()

    def create_widgets(self):
        frm = ttk.Frame(self.root, padding=10)
        frm.grid()

        ttk.Label(frm, text="Source Account:").grid(column=0, row=0, sticky="w")
        self.src_combo = ttk.Combobox(frm, textvariable=self.src_var, state="readonly")
        self.src_combo['values'] = [f"{u[1]} ({u[0]})" for u in self.users]  # Display username and steamid3
        self.src_combo.grid(column=1, row=0)
        self.src_combo.bind("<<ComboboxSelected>>", lambda e: self.update_avatar("src"))

        ttk.Label(frm, text="Destination Account:").grid(column=0, row=1, sticky="w")
        self.dst_combo = ttk.Combobox(frm, textvariable=self.dst_var, state="readonly")
        self.dst_combo['values'] = [f"{u[1]} ({u[0]})" for u in self.users]  # Display username and steamid3
        self.dst_combo.grid(column=1, row=1)
        self.dst_combo.bind("<<ComboboxSelected>>", lambda e: self.update_avatar("dst"))

        # Avatar previews
        self.avatar_src_lbl = ttk.Label(frm)
        self.avatar_src_lbl.grid(column=2, row=0, padx=10)

        self.avatar_dst_lbl = ttk.Label(frm)
        self.avatar_dst_lbl.grid(column=2, row=1, padx=10)

        # Set default placeholder images
        placeholder = download_placeholder()
        self.avatar_src_lbl.configure(image=placeholder)
        self.avatar_src_lbl.image = placeholder
        self.avatar_dst_lbl.configure(image=placeholder)
        self.avatar_dst_lbl.image = placeholder

        self.copy_btn = ttk.Button(frm, text="Copy Userdata", command=self.confirm_copy)
        self.copy_btn.grid(column=0, row=2, columnspan=3, pady=10)

        # Add current path info
        path_info = ttk.Label(
            frm,
            text=f"Steam userdata path: {STEAM_USERDATA_PATH}",
            font=('TkDefaultFont', 8)
        )
        path_info.grid(column=0, row=3, columnspan=3, sticky="w")

    def update_avatar(self, which):
        idx = self.src_combo.current() if which == "src" else self.dst_combo.current()
        if idx < 0 or idx >= len(self.users):
            return
        avatar_hash = self.users[idx][2]
        image = download_avatar(avatar_hash)
        if image:
            if which == "src":
                self.avatar_src_lbl.configure(image=image)
                self.avatar_src_lbl.image = image
            else:
                self.avatar_dst_lbl.configure(image=image)
                self.avatar_dst_lbl.image = image

    def confirm_copy(self):
        src_idx = self.src_combo.current()
        dst_idx = self.dst_combo.current()

        if src_idx < 0 or dst_idx < 0:
            messagebox.showerror("Error", "Please select both source and destination.")
            return

        src_id = self.users[src_idx][0]
        dst_id = self.users[dst_idx][0]
        src_name = self.users[src_idx][1]
        dst_name = self.users[dst_idx][1]

        if src_id == dst_id:
            messagebox.showerror("Error", "Source and destination cannot be the same.")
            return

        # Show confirmation dialog
        confirm = messagebox.askyesno(
            "Confirm Copy",
            f"Are you sure you want to copy all game data from:\n"
            f"Source: {src_name} ({src_id})\n"
            f"Destination: {dst_name} ({dst_id})\n\n"
            f"This will overwrite any existing data in the destination account!",
            icon='warning'
        )
        
        if confirm:
            try:
                if copy_numeric_folders(src_id, dst_id):
                    messagebox.showinfo("Success", f"Successfully copied data from {src_name} → {dst_name}")
            except Exception as e:
                messagebox.showerror("Error", str(e))

if __name__ == "__main__":
    root = tk.Tk()
    app = SteamCopierApp(root)
    root.mainloop()
