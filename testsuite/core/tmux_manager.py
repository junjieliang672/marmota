import libtmux
import time 
import logging
import core.keys as keys
from core.marmota_manager import BaseMarmotaManager

class TmuxMarmotaManager(BaseMarmotaManager):
    """
    Tmux based Manager
    After running marmota, you can connect to the session via
    tmux -L marmota-testsuite attach -t marmota_session
    Wont work in windows
    """
    # Class variables
    MARMOTA_START_DELAY : float = 0.1 # seconds
    MARMOTA_SOCKET_NAME : str = "marmota-testsuite"

    # Init should not allocate any resources
    def __init__(self, marmota_path : str):
        super().__init__(marmota_path)
        
        # Check libtmux version requirement
        min_version = (0, 31, 0)
        current_version_str = libtmux.__version__
        
        # Parse version string to tuple for comparison
        try:
            current_version = tuple(map(int, current_version_str.split('.')[:3]))
        except (ValueError, AttributeError):
            current_version = (0, 0, 0)
        
        if current_version < min_version:
            raise RuntimeError(
                f"libtmux version 0.31.0 or higher is required. "
                f"Current version: {current_version_str}. "
                f"Please upgrade with: pip install 'libtmux>=0.31.0'"
            )
        
        self.logger = logging.getLogger()
        self.server = libtmux.Server(socket_name=TmuxMarmotaManager.MARMOTA_SOCKET_NAME)
        self.logger.debug("server object : %s", self.server)
        self.marmota_session : libtmux.Session = None
        self.marmota_pane : libtmux.Pane = None

    def start_marmota(self, start_dir : str = None, args : list[str] = None) -> None:
        marmota_command = self.marmota_path
        if args:
            marmota_command += " " + " ".join(args)

        self.logger.debug("windows_command : %s", marmota_command)
        

        self.marmota_session= self.server.new_session('marmota_session',
                window_command=marmota_command, 
                start_directory=start_dir)
        time.sleep(TmuxMarmotaManager.MARMOTA_START_DELAY)
        self.logger.debug("marmota_session initialised : %s", self.marmota_session)

        # If libtmux version is less than 0.3.1, active_pane does not exist.
        self.marmota_pane = self.marmota_session.active_pane
        self._is_marmota_running = True

    def _send_key(self, key : str) -> None:
        self.logger.debug("sending key : %s", repr(key))
        self.marmota_pane.send_keys(key, enter=False)

    def send_text_input(self, text : str, all_at_once : bool = True) -> None:
        if all_at_once:
            self._send_key(text)
        else:
            for c in text:
                self._send_key(c)

    def send_special_input(self, key : keys.Keys) -> str:
        if key.ascii_code != keys.NO_ASCII:
            self._send_key(chr(key.ascii_code))
        elif isinstance(key, keys.SpecialKeys):
            self._send_key(key.key_name)
        else:
            raise Exception(f"Unknown key : {key}") 
            
    def get_rendered_output(self) -> str:
        return "[Not supported yet]"

    def is_marmota_running(self) -> bool:
        self._is_marmota_running = (self.marmota_session is not None) \
            and (self.marmota_session in self.server.sessions)

        return self._is_marmota_running

    def close_marmota(self) -> None:
        if self.is_marmota_running():
            self.server.kill_session(self.marmota_session.name)

    # Override
    def runtime_info(self) -> str:
        return str(self.server.sessions)

    def __repr__(self) -> str:
        return f"{self.__class__.__name__}(server : {self.server}, " + \
            f"session : {self.marmota_session}, running : {self._is_marmota_running})"
